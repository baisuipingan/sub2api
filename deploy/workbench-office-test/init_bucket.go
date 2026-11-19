// Independent test bucket bootstrap. It uses the project's existing AWS SDK;
// credentials are test-only environment values and are never printed.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	password := os.Getenv("MINIO_ROOT_PASSWORD")
	if len(password) < 32 {
		panic("test S3 secret is missing")
	}
	ready := false
	client := &http.Client{Timeout: 2 * time.Second}
	for i := 0; i < 60; i++ {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://minio:9000/minio/health/live", nil)
		response, err := client.Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(time.Second)
	}
	if !ready {
		panic("test MinIO did not become healthy")
	}
	store := s3.New(s3.Options{Region: "us-east-1", BaseEndpoint: aws.String("http://minio:9000"), UsePathStyle: true, Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("office-test", password, ""))})
	if _, err := store.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String("office-test")}); err != nil {
		if _, err = store.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("office-test")}); err != nil {
			panic("create isolated test bucket failed")
		}
	}
	if _, err := store.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{Bucket: aws.String("office-test")}); err != nil {
		panic("could not ensure private test bucket")
	}
	response, err := client.Get("http://minio:9000/office-test/")
	if err != nil {
		panic("private test bucket probe failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		panic("test bucket unexpectedly permits anonymous listing")
	}
	fmt.Println("isolated office-test bucket ready; anonymous access denied")
}
