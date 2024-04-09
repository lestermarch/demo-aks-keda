package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue"
)

func main() {
	// The storage account to connect to
	accountName, ok := os.LookupEnv("STORAGE_ACCOUNT_NAME")
	if !ok {
		panic("Environment variable STORAGE_ACCOUNT_NAME could not be found.")
	}

	serviceUrl := fmt.Sprintf("https://%s.queue.core.windows.net/", accountName)

	// The storage account queue to send to
	queueName, ok := os.LookupEnv("STORAGE_QUEUE_NAME")
	if !ok {
		panic("Environment variable STORAGE_QUEUE_NAME could not be found.")
	}

	// The minimum number of message to send each minute
	messageCountMinEnv, ok := os.LookupEnv("MESSAGE_COUNT_PER_MINUTE_MIN")
	var messageCountMin int
	if ok {
		var err error
		messageCountMin, err = strconv.Atoi(messageCountMinEnv)
		handleError(err)
	}

	// The minimum number of message to send each minute
	messageCountMaxEnv, ok := os.LookupEnv("MESSAGE_COUNT_PER_MINUTE_MAX")
	var messageCountMax int
	if ok {
		var err error
		messageCountMax, err = strconv.Atoi(messageCountMaxEnv)
		handleError(err)
	}

	// Authenticate to the storage account
	tokenCredential, err := azidentity.NewDefaultAzureCredential(nil)
	handleError(err)

	client, err := azqueue.NewServiceClient(serviceUrl, tokenCredential, nil)
	handleError(err)

	fmt.Printf("Connected to storage account: %s\n", accountName)

	// Generate messages
	hostname, err := os.Hostname()
	handleError(err)

	queueClient := client.NewQueueClient(queueName)
	opts := &azqueue.EnqueueMessageOptions{TimeToLive: to.Ptr(int32(7200))} // 2 hours

	var b int = 1
	for {
		rand.Seed(time.Now().UnixNano())
		messageCount := rand.Intn(messageCountMax-messageCountMin+1) + messageCountMin

		batch := strconv.Itoa(b)
		messageCountStr := strconv.Itoa(messageCount)

		fmt.Printf("Sending batch %s of %s messages to %s/%s\n", batch, messageCountStr, accountName, queueName)

		for i := 1; i <= messageCount; i++ {
			index := strconv.Itoa(i)

			messageContent := fmt.Sprintf("%s-%s from %s\n", batch, index, hostname)
			_, err := queueClient.EnqueueMessage(context.Background(), messageContent, opts)
			handleError(err)

			fmt.Printf("Sent %s-%s to %s/%s\n", batch, index, accountName, queueName)
		}

		fmt.Println("Sleeping for 1 minute")
		time.Sleep(60 * time.Second)
		b++
	}
}

func handleError(err error) {
	if err != nil {
		log.Fatal(err.Error())
	}
}
