package main

import (
	"context"
	//"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"google.golang.org/grpc"
	//"google.golang.org/grpc/credentials"

	pb "github.com/tiket/TIX-COMMON-GO/price_integrator"
)

// const (
// 	targetAddress     = "192.168.88.91:9999"
// 	timeout           = 5000 * time.Millisecond
// 	testDuration      = 5 * time.Minute
// 	initialRPS        = 0
// 	maxRPS            = 500
// 	stepRPS           = 100
// 	stepDuration      = 1 * time.Minute
// 	concurrentWorkers = 1300 
// )

const (
	targetAddress     = "192.168.88.91:9999"
	timeout           = 5000 * time.Millisecond
	testDuration      = 5 * time.Minute
	initialRPS        = 0          
	maxRPS            = 17        
	stepRPS           = 5           
	stepDuration      = 1 * time.Minute
	concurrentWorkers = 100         
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
		results <- callService(client, idx)
	}
}

func main() {
	//creds := credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})
	conn, err := grpc.Dial(targetAddress, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Gagal connect ke GRPC: %v", err)
	}
	defer conn.Close()

	client := pb.NewRpcIntegratorClient(conn)

	jobs := make(chan int)
	results := make(chan Result, 1000000)

	var wg sync.WaitGroup
	for i := 0; i < concurrentWorkers; i++ {
		wg.Add(1)
		go worker(&wg, client, jobs, results)
	}

	go func() {
		defer close(jobs)

		start := time.Now()
		i := 0
		currentRPS := initialRPS
		lastStepTime := time.Now()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for time.Since(start) < testDuration {
			<-ticker.C
			for j := 0; j < currentRPS; j++ {
				jobs <- i
				i++
			}

			if time.Since(lastStepTime) >= stepDuration && currentRPS < maxRPS {
				currentRPS += stepRPS
				if currentRPS > maxRPS {
					currentRPS = maxRPS
				}
				fmt.Printf("🚀 Ramping up... current RPS: %d\n", currentRPS)
				lastStepTime = time.Now()
			}
		}
	}()

	wg.Wait()
	close(results)

	allResults := make([]Result, 0, 1000000)
	for res := range results {
		allResults = append(allResults, res)
	}

	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Index < allResults[j].Index
	})

	fmt.Println("\n📊 Latency per Request:")
	var okCount, failCount int
	for _, r := range allResults {
		status := "OK"
		if r.Error != nil {
			status = r.Error.Error()
			failCount++
		} else {
			okCount++
		}
		fmt.Printf("Request #%06d | Latency: %-10v | Status: %s\n", r.Index, r.Latency, status)
	}

	total := okCount + failCount
	okPercent := float64(okCount) / float64(total) * 100
	failPercent := float64(failCount) / float64(total) * 100

	fmt.Printf("\n✅ Sukses: %d (%.2f%%)\n", okCount, okPercent)
	fmt.Printf("❌ Gagal : %d (%.2f%%)\n", failCount, failPercent)
	fmt.Printf("📈 Total Requests Dikirim: %d\n", total)
}
