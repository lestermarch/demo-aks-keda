package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

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

	// The simulated time to process each message in seconds
	processingTimeSecondsEnv, ok := os.LookupEnv("MESSAGE_PROCESSING_SECONDS")
	if !ok {
		panic("Environment variable MESSAGE_PROCESSING_SECONDS could not be found.")
	}

	processingTimeSeconds, err := strconv.Atoi(processingTimeSecondsEnv)
	handleError(err)

	processingTimeDuration := time.Duration(processingTimeSeconds) * time.Second

	// Authenticate to the storage account
	tokenCredential, err := azidentity.NewDefaultAzureCredential(nil)
	handleError(err)

	client, err := azqueue.NewServiceClient(serviceUrl, tokenCredential, nil)
	handleError(err)

	fmt.Printf("Connected to storage account: %s\n", accountName)

	// Process messages
	queueClient := client.NewQueueClient(queueName)

	for {
		dqResponse, err := queueClient.DequeueMessage(context.Background(), nil)
		handleError(err)

		if len(dqResponse.Messages) == 0 {
			fmt.Println("No messages to process. Retrying in 20 seconds.")
			time.Sleep(20 * time.Second)
			continue
		}

		messageContent := *dqResponse.Messages[0].MessageText
		messageId := *dqResponse.Messages[0].MessageID
		messageInsertionTime := *dqResponse.Messages[0].InsertionTime

		fmt.Printf("\nProcessing message %s from %s\n", messageId, messageInsertionTime)
		fmt.Println(strings.TrimSuffix(messageContent, "\n"))
		time.Sleep(processingTimeDuration) // Simulate processing

		messageReceipt := *dqResponse.Messages[0].PopReceipt
		delResponse, err := queueClient.DeleteMessage(context.Background(), messageId, messageReceipt, nil)
		handleError(err)

		deleteDate := *delResponse.Date

		fmt.Printf("Deleted message %s at %s\n", messageId, deleteDate)
	}
}

func handleError(err error) {
	if err != nil {
		log.Fatal(err.Error())
	}
}
