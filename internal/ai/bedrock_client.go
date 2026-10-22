package ai

import (
	"context"
	"errors"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

func newBedrockClient(ctx context.Context, region string) (*bedrockruntime.Client, error) {
	if region == "" {
		return nil, errors.New("AWS region is required for the bedrock provider (set ai.region)")
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config for bedrock: %w", err)
	}
	return bedrockruntime.NewFromConfig(cfg), nil
}
