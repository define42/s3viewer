package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func loadAWSConfigWithStaticCredentials(ctx context.Context, region, endpoint, accessKey, secretKey, sessionToken string, endpointSkipTls bool, useRgwToken bool) (aws.Config, error) {
	if strings.TrimSpace(accessKey) == "" || strings.TrimSpace(secretKey) == "" {
		return aws.Config{}, fmt.Errorf("access key and secret key are required")
	}

	if useRgwToken {
		var err error
		accessKey, err = generateRGWToken(accessKey, secretKey)
		if err != nil {
			return aws.Config{}, fmt.Errorf("generateRGWToken failed: %w", err)
		}
	}

	var optFns []func(*config.LoadOptions) error
	optFns = append(optFns,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, sessionToken)),
	)

	// Skip TLS verification (ONLY if requested)
	if endpointSkipTls {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{}
		}
		tr.TLSClientConfig.InsecureSkipVerify = true

		optFns = append(optFns, config.WithHTTPClient(&http.Client{Transport: tr}))
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
func newS3Client(ctx context.Context, region, endpoint string, forcePathStyle bool, accessKey, secretKey, sessionToken string, endpointSkipTls bool, useRgwToken bool) (*s3.Client, error) {

	cfg, err := loadAWSConfigWithStaticCredentials(ctx, region, endpoint, accessKey, secretKey, sessionToken, endpointSkipTls, useRgwToken)
	if err != nil {
		return nil, err
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = forcePathStyle
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationUnset
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationUnset
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
