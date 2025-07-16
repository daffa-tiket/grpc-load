package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/tiket/TIX-COMMON-GO/price_integrator"
)

const (
	targetAddress   = "hotel-integrator-framework-webbeds-svc-grpc.prod-hotel-cluster.tiket.com:443" 
	totalRequests   = 250000
	concurrentUsers = 1000
	timeout         = 2500 * time.Millisecond
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

	req := []byte(`{
  "mandatoryRequest": {
    "storeID": "TIKETCOM",
    "channelID": "DESKTOP",
    "requestID": "test_agoda_avail",
    "serviceID": "gateway",
    "accountID": "16124",
    "username": "norma.puspitasari@tiket.com",
    "currency": "IDR",
    "businessID": "1",
    "loginMedia": "GOOGLE",
    "forwardedFor": "127.0.0.1",
    "trueClientIP": "127.0.0.1",
    "language": "en",
    "login": 1,
    "isVerifiedPhoneNumber": "0",
    "loyaltyLevel": "LV4",
    "countryID": "en"
  },
  "hotelAvailabilityRequest": {
    "hotelIds": {
      "5291445": "5291445"
    },
    "startDate": "2025-10-23",
    "endDate": "2025-10-24",
    "numberOfNights": 1,
    "numberOfRooms": 1,
    "numberOfAdults": 1,
    "vendor": "WEBBEDS",
    "localTimezone": "+07:00",
    "rateKeyMapping": [
      {
        "rateKeyType": "member_rate"
      }
    ]
  },
  "route": "hotel-list"
}`)

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
	creds := credentials.NewTLS(&tls.Config{
				InsecureSkipVerify: true,
			})
	conn, err := grpc.Dial(targetAddress, grpc.WithTransportCredentials(creds))
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
	var okCount, failCount int
	for _, r := range allResults {
		status := "OK"
		if r.Error != nil {
			status = r.Error.Error()
			failCount++
		} else {
			okCount++
		}
		fmt.Printf("Request #%03d | Latency: %-10v | Status: %s\n", r.Index, r.Latency, status)
	}

	total := okCount + failCount
	okPercent := float64(okCount) / float64(total) * 100
	failPercent := float64(failCount) / float64(total) * 100

	fmt.Printf("\n✅ Sukses: %d (%.2f%%)\n", okCount, okPercent)
	fmt.Printf("❌ Gagal : %d (%.2f%%)\n", failCount, failPercent)
}