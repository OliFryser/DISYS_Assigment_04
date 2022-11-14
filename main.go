package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
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

	/*Parses id to string for logfile name
	idString = strconv.Itoa(*p.id)

	//Prints to log file instead of terminal

	f, err := os.OpenFile("logfile."+*p.idString, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()
	log.SetOutput(f)*/

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
		p.IncrementLamportTime(0)
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

func (p *peer) IncrementLamportTime(otherLamportTime int32) {
	var mu sync.Mutex
	defer mu.Unlock()
	mu.Lock()
	if p.lamportTime < otherLamportTime {
		p.lamportTime = otherLamportTime + 1
	} else {
		p.lamportTime++
	}
}

func (p *peer) RequestedAccess(ctx context.Context, req *consensus.Request) (*consensus.Reply, error) {
	log.Printf("Received access request from peer %d (Lamport time %d)\n", req.Id, req.LamportTime)
	//This if statement looks like a mess. It checks if the state is held or if the state is wanted AND if our Lamport time is greater than the requested
	//OR if it is the same and our id is less than the other one.
	if p.state == HELD || (p.state == WANTED && (p.lamportTime < req.LamportTime || (p.lamportTime == req.LamportTime && p.id < req.Id))) {
		p.requestQueue = append(p.requestQueue, req.Id) //If so add request to queue
		<-p.queue[req.Id]                               //Channel blocks until we reply to queue
	}
	//Reply to request
	reply := &consensus.Reply{
		LamportTime: p.lamportTime,
		Id:          p.id,
	}
	p.IncrementLamportTime(req.LamportTime)
	log.Printf("Reply access request from peer %d (Lamport time %d)\n", req.Id, p.lamportTime)
	return reply, nil
}

func (p *peer) requestAccessFromAll() {
	p.state = WANTED
	log.Printf("State is WANTED\n")
	//Request access from all peers and wait for reply
	var wg sync.WaitGroup
	for id, client := range p.clients {
		p.IncrementLamportTime(0)
		request := &consensus.Request{LamportTime: p.lamportTime, Id: p.id}
		wg.Add(1)
		log.Printf("Requesting access from peer %d (Lamport time: %d)\n", id, p.lamportTime)
		go func(requestId int32, requestClient consensus.ConsensusClient) {
			defer wg.Done()
			reply, err := requestClient.RequestedAccess(p.ctx, request)
			if err != nil {
				fmt.Println("Something went wrong")
			}
			p.IncrementLamportTime(reply.LamportTime)
			log.Printf("Got reply from peer %d (Lamport time: %d)\n", reply.Id, p.lamportTime)
		}(id, client)
	}
	wg.Wait()

	//Access the critical section
	p.state = HELD
	log.Printf("State is HELD\n")
	writeToCriticalSection(p)
	p.state = RELEASED
	log.Printf("State is RELEASED\n")

	//Reply to all peers in queue by unblocking the channels
	for len(p.requestQueue) != 0 {
		requestId := p.requestQueue[0]
		p.queue[requestId] <- true
		p.requestQueue = p.requestQueue[1:]
	}
}

func writeToCriticalSection(p *peer) {
	filepath := "./CRITICAL_SECTION/Shared-file.txt"

	//Create Shared-file.txt
	sharedFile, err := os.Create(filepath)
	if err != nil {
		log.Fatalf("Could not write to file.\n")
	}

	// Sleep to simulate longer access
	time.Sleep(5000 * time.Millisecond)

	//Write portnumber to Shared-file.txt
	sharedFile.WriteString(fmt.Sprintf("port %d is the best", p.id))
}
