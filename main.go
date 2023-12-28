package main

import (
	"context"
	"fmt"
	"github.com/aws/aws-lambda-go/lambda"
	"log"
	"math"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/sod-auctions/blizzard-client"
	"github.com/sod-auctions/file-writer"
)

func toTimeLeftEnum(timeLeft string) int32 {
	if timeLeft == "SHORT" {
		return 1
	}
	if timeLeft == "MEDIUM" {
		return 2
	}
	if timeLeft == "LONG" {
		return 3
	}
	if timeLeft == "VERY_LONG" {
		return 4
	}
	return 0
}

func fetchAuctions() (*[]blizzard_client.Auction, error) {
	client := blizzard_client.NewBlizzardClient(os.Getenv("BLIZZARD_CLIENT_ID"), os.Getenv("BLIZZARD_CLIENT_SECRET"))
	wildGrowthRealmId := int64(5813)
	allianceAuctionHouseId := int64(2)

	auctions, err := client.GetAuctions(wildGrowthRealmId, allianceAuctionHouseId)
	if err != nil {
		return nil, err
	}

	return auctions, nil
}

func writeToFile(auctions *[]blizzard_client.Auction) error {
	writer := file_writer.NewFileWriter("/tmp/data.parquet")
	wildGrowthRealmId := int64(5813)
	allianceAuctionHouseId := int64(2)

	for _, auction := range *auctions {
		err := writer.Write(&file_writer.Record{
			RealmID:        int32(wildGrowthRealmId),
			AuctionHouseID: int32(allianceAuctionHouseId),
			ItemID:         int32(auction.ItemId),
			Bid:            int32(auction.Bid),
			Buyout:         int32(auction.Buyout),
			BuyoutEach:     int32(math.Round(float64(auction.Buyout) / float64(auction.Quantity))),
			Quantity:       int32(auction.Quantity),
			TimeLeft:       toTimeLeftEnum(auction.TimeLeft),
		})

		if err != nil {
			return err
		}
	}
	return writer.Close()
}

func uploadToS3() (*s3manager.UploadOutput, error) {
	sess := session.Must(session.NewSession())
	uploader := s3manager.NewUploader(sess)

	file, err := os.Open("/tmp/data.parquet")
	if err != nil {
		return nil, err
	}

	currentTime := time.Now().UTC()
	dir := fmt.Sprintf("data/year=%d/month=%02d/day=%02d/hour=%02d/",
		currentTime.Year(), currentTime.Month(), currentTime.Day(), currentTime.Hour())

	return uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String("sod-auctions"),
		Key:    aws.String(dir + "data.parquet"),
		Body:   file,
	})
}

func handler(ctx context.Context, event interface{}) error {
	log.Println("fetching auctions...")
	auctions, err := fetchAuctions()
	if err != nil {
		return fmt.Errorf("error occurred while getting auctions: %s", err)
	}

	log.Printf("writing %d auctions to file...", len(*auctions))
	err = writeToFile(auctions)
	if err != nil {
		return fmt.Errorf("error occurred while writing to file: %s", err)
	}

	log.Println("uploading file to S3...")
	output, err := uploadToS3()
	if err != nil {
		return fmt.Errorf("error occurred while uploading file to s3: %s", err)
	}

	log.Printf("successfully uploaded file %s (%s)", output.Location, output.UploadID)
	return nil
}

func main() {
	lambda.Start(handler)
}
