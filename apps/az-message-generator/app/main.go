package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue"
)

func main() {
	accountName, ok := os.LookupEnv("STORAGE_ACCOUNT_NAME")
	if !ok {
		panic("Environment variable STORAGE_ACCOUNT_NAME could not be found.")
	}

	serviceUrl := fmt.Sprintf("https://%s.queue.core.windows.net/", accountName)

	queueName, ok := os.LookupEnv("STORAGE_QUEUE_NAME")
	if !ok {
		panic("Environment variable STORAGE_QUEUE_NAME could not be found.")
	}

	messageCountEnv, ok := os.LookupEnv("MESSAGE_COUNT_PER_MINUTE")
	var messageCount int = 10
	if ok {
		var err error
		messageCount, err = strconv.Atoi(messageCountEnv)
		handleError(err)
	}

	// Authenticate
	tokenCredential, err := azidentity.NewDefaultAzureCredential(nil)
	handleError(err)

	client, err := azqueue.NewServiceClient(serviceUrl, tokenCredential, nil)
	handleError(err)

	fmt.Printf("Connected to storage account: %s\n", accountName)

	// Generate messages
	hostname, err := os.Hostname()
	handleError(err)

	queueClient := client.NewQueueClient(queueName)
	opts := &azqueue.EnqueueMessageOptions{TimeToLive: to.Ptr(int32(86400))} // 1 day

	var batch int = 1
	for {
		for i := 1; i <= messageCount; i++ {
			batch := strconv.Itoa(batch)
			index := strconv.Itoa(i)

			messageContent := fmt.Sprintf("%s-%s from %s\n", batch, index, hostname)
			_, err := queueClient.EnqueueMessage(context.Background(), messageContent, opts)
			handleError(err)

			fmt.Printf("Sent batch %s message %s to %s/%s\n", batch, index, accountName, queueName)
		}

		fmt.Println("Sleeping for 1 minute")
		time.Sleep(60 * time.Second)
		batch++
	}
}

func handleError(err error) {
	if err != nil {
		log.Fatal(err.Error())
	}
}
