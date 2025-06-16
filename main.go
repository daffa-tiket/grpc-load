package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"google.golang.org/grpc"

	pb "github.com/tiket/TIX-COMMON-GO/price_integrator"
)

const (
	targetAddress   = "localhost:9999" 
	concurrentUsers = 20
	totalRequests   = 100
	timeout         = 20 * time.Second
)

type Result struct {
	Latency   time.Duration
	Error     error
	Timestamp time.Time
	Index     int
}

func callService(client pb.RpcIntegratorClient, idx int) Result {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()

	req := []byte(``)

	var user pb.HotelAvailPriceRequest
	err := json.Unmarshal(req, &user)
	if err != nil {
		return Result{
			Latency:   0,
			Error:     fmt.Errorf("gagal unmarshal request: %v", err),
			Timestamp: time.Now(),
			Index:     idx,
		}
	}

	_, errs := client.DoAvail(ctx, &user)

	latency := time.Since(start)
	return Result{
		Latency:   latency,
		Error:     errs,
		Timestamp: time.Now(),
		Index:     idx,
	}
}

func worker(wg *sync.WaitGroup, client pb.RpcIntegratorClient, jobs <-chan int, results chan<- Result) {
	defer wg.Done()
	for idx := range jobs {
		res := callService(client, idx)
		results <- res
	}
}

func main() {
	conn, err := grpc.Dial(targetAddress, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("failed connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewRpcIntegratorClient(conn)

	jobs := make(chan int, totalRequests)
	results := make(chan Result, totalRequests)

	var wg sync.WaitGroup
	for i := 0; i < concurrentUsers; i++ {
		wg.Add(1)
		go worker(&wg, client, jobs, results)
	}

	for i := 0; i < totalRequests; i++ {
		jobs <- i
	}
	close(jobs)

	wg.Wait()
	close(results)

	allResults := make([]Result, 0, totalRequests)
	for res := range results {
		allResults = append(allResults, res)
	}

	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Index < allResults[j].Index
	})

	fmt.Println("\n📊 Latency per Request:")
	for _, r := range allResults {
		status := "OK"
		if r.Error != nil {
			status = r.Error.Error()
		}
		fmt.Printf("Request #%03d | Latency: %-10v | Status: %s\n", r.Index, r.Latency, status)
	}
}