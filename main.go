package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"time"

	consensus "github.com/OliFryser/DISYS_Assigment_04/grpc"
	"google.golang.org/grpc"
)

func main() {
	arg1, _ := strconv.ParseInt(os.Args[1], 10, 32)
	ownPort := int32(arg1) + 5000

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := &peer{
		id:           ownPort,
		lamportTime:  0,
		state:        RELEASED,
		queue:        make(map[int32]chan bool),
		requestQueue: make([]int32, 0),
		clients:      make(map[int32]consensus.ConsensusClient),
		ctx:          ctx,
	}

	// Create listener tcp on port ownPort
	list, err := net.Listen("tcp", fmt.Sprintf("localhost:%v", ownPort))
	if err != nil {
		log.Fatalf("Failed to listen on port: %v", err)
	}
	grpcServer := grpc.NewServer()
	consensus.RegisterConsensusServer(grpcServer, p)

	go func() {
		if err := grpcServer.Serve(list); err != nil {
			log.Fatalf("failed to server %v", err)
		}
	}()

	for i := 0; i < 3; i++ {
		port := int32(5000) + int32(i)

		if port == ownPort {
			continue
		}

		p.queue[port] = make(chan bool, 10)

		var conn *grpc.ClientConn
		fmt.Printf("Trying to dial: %v\n", port)
		conn, err := grpc.Dial(fmt.Sprintf("localhost:%v", port), grpc.WithInsecure(), grpc.WithBlock())
		if err != nil {
			log.Fatalf("Could not connect: %s", err)
		}
		defer conn.Close()
		c := consensus.NewConsensusClient(conn)
		p.clients[port] = c
	}
	log.Printf("My id: %d", p.id)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		p.requestAccessFromAll()
	}
}

type peer struct {
	consensus.UnimplementedConsensusServer
	id           int32
	lamportTime  int32
	state        State
	queue        map[int32]chan bool
	requestQueue []int32
	clients      map[int32]consensus.ConsensusClient
	ctx          context.Context
}

type State int32

const (
	HELD State = iota
	RELEASED
	WANTED
)

func (p *peer) RequestedAccess(ctx context.Context, req *consensus.Request) (*consensus.Reply, error) {
	if p.state == HELD || (p.state == WANTED && p.lamportTime < req.LamportTime) {
		p.requestQueue = append(p.requestQueue, req.Id)
		<-p.queue[req.Id]
	}
	reply := &consensus.Reply{
		LamportTime: p.lamportTime,
		Id:          p.id,
	}
	return reply, nil
}

func (p *peer) requestAccessFromAll() {
	p.state = WANTED
	request := &consensus.Request{LamportTime: p.lamportTime, Id: p.id}
	for id, client := range p.clients {
		go func() {
			log.Printf("Requesting access from peer %d\n", id)
			reply, err := client.RequestedAccess(p.ctx, request)
			if err != nil {
				fmt.Println("Something went wrong")
			}
			log.Printf("Got reply from peer %d\n", reply.Id)
		}()
	}
	p.state = HELD
	log.Printf("State is HELD\n")
	writeToCriticalSection(p)
	p.state = RELEASED
	log.Printf("State is RELEASED\n")
	for len(p.requestQueue) != 0 {
		requestId := p.requestQueue[0]
		p.queue[requestId] <- true
		p.requestQueue = p.requestQueue[1:]
	}
}

func writeToCriticalSection(p *peer) {
	filepath := "./CRITICAL_SECTION/Shared-file.txt"

	log.Printf("Trying to write to shared file.\n")

	//Create Shared-file.txt
	sharedFile, err := os.Create(filepath)
	if err != nil {
		log.Fatalf("Could not write to file.\n")
	}

	//Write portnumber to Shared-file.txt
	sharedFile.WriteString(fmt.Sprintf("port %d is the best", p.id))
	log.Printf("Succesfully wrote to shared file.\n")

	//Sleep to simulate longer access time
	time.Sleep(2000 / time.Millisecond)
}
