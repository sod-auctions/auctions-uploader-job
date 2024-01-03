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
	"github.com/sod-auctions/auctions-db"
	"github.com/sod-auctions/blizzard-client"
	"github.com/sod-auctions/file-writer"
)

var database *auctions_db.Database
var client *blizzard_client.BlizzardClient

func init() {
	log.SetFlags(0)

	var err error
	database, err = auctions_db.NewDatabase(os.Getenv("DB_CONNECTION_STRING"))
	if err != nil {
		panic(err)
	}

	client = blizzard_client.NewBlizzardClient(os.Getenv("BLIZZARD_CLIENT_ID"), os.Getenv("BLIZZARD_CLIENT_SECRET"))
}

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

func fetchAndWriteAuctions(writer *file_writer.FileWriter, realmId int16, auctionHouseId int16) error {
	auctions, err := client.GetAuctions(int64(realmId), int64(auctionHouseId))
	if err != nil {
		return err
	}

	log.Printf("writing %d auctions to file\n", len(auctions))
	for _, auction := range auctions {
		err := writer.Write(&file_writer.Record{
			RealmID:        int32(realmId),
			AuctionHouseID: int32(auctionHouseId),
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

	return nil
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
	writer := file_writer.NewFileWriter("/tmp/data.parquet")

	log.Println("fetching realms...")
	realms, err := database.GetRealms()
	log.Println("fetching auction houses...")
	auctionHouses, err := database.GetAuctionHouses()
	for _, realm := range realms {
		for _, auctionHouse := range auctionHouses {
			log.Printf("fetching auctions for realm %s (%d), auction house %s (%d)\n",
				realm.Name, realm.Id, auctionHouse.Name, auctionHouse.Id)
			err := fetchAndWriteAuctions(writer, realm.Id, auctionHouse.Id)
			if err != nil {
				return fmt.Errorf("error occurred while retrieving/writing auctions: %v", err)
			}
		}
	}

	log.Println("flushing results to file...")
	err = writer.Close()
	if err != nil {
		return fmt.Errorf("error occurred while flushing results to file: %v", err)
	}

	log.Println("uploading file to S3...")
	output, err := uploadToS3()
	if err != nil {
		return fmt.Errorf("error occurred while uploading file to s3: %v", err)
	}

	log.Printf("successfully uploaded file %s (%s)\n", output.Location, output.UploadID)
	return nil
}

func main() {
	lambda.Start(handler)
}
