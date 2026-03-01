package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func loadAWSConfigWithStaticCredentials(ctx context.Context, region, endpoint, accessKey, secretKey, sessionToken string) (aws.Config, error) {
	if strings.TrimSpace(accessKey) == "" || strings.TrimSpace(secretKey) == "" {
		return aws.Config{}, fmt.Errorf("access key and secret key are required")
	}

	var optFns []func(*config.LoadOptions) error
	optFns = append(optFns,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, sessionToken)),
	)

	awsCfg, err := config.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		return aws.Config{}, err
	}

	if endpoint != "" {
		ep, err := url.Parse(endpoint)
		if err != nil {
			return aws.Config{}, fmt.Errorf("invalid AWS_ENDPOINT_URL: %w", err)
		}
		awsCfg.BaseEndpoint = aws.String(ep.String())
	}

	return awsCfg, nil
}

func newS3Client(ctx context.Context, region, endpoint string, forcePathStyle bool, accessKey, secretKey, sessionToken string) (*s3.Client, error) {
	cfg, err := loadAWSConfigWithStaticCredentials(ctx, region, endpoint, accessKey, secretKey, sessionToken)
	if err != nil {
		return nil, err
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = forcePathStyle
	})
	return client, nil
}

func getenv(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func getenvAny(def string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return def
}
