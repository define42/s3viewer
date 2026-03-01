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
)

// Env:
//
//	AWS_REGION or S3_REGION (default eu-west-1)
//	LISTEN_ADDR (default :8080)
//	AWS_ENDPOINT_URL or S3_ENDPOINT (optional for MinIO/S3-compatible)
//	S3_FORCE_PATH_STYLE ("true" commonly for MinIO)
//	AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_SESSION_TOKEN
//	or S3_ACCESS_KEY / S3_SECRET_KEY (optional; else default chain)
func loadAWSConfig(ctx context.Context, region string) (aws.Config, error) {
	endpoint := getenvAny("", "AWS_ENDPOINT_URL", "S3_ENDPOINT")

	var optFns []func(*config.LoadOptions) error
	optFns = append(optFns, config.WithRegion(region))

	ak := getenvAny("", "AWS_ACCESS_KEY_ID", "S3_ACCESS_KEY")
	sk := getenvAny("", "AWS_SECRET_ACCESS_KEY", "S3_SECRET_KEY")
	st := getenvAny("", "AWS_SESSION_TOKEN")
	if ak != "" && sk != "" {
		optFns = append(optFns, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(ak, sk, st)))
	}

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
