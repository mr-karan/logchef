package ai

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// bedrockConverseAPI is the subset of the bedrockruntime client used here.
// Declaring it as an interface keeps the provider unit-testable.
type bedrockConverseAPI interface {
	Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
}

// bedrockProvider is the native AWS Bedrock transport. It uses the unified
// Converse API (works across Claude/Llama/Titan/Nova/Mistral) and authenticates
// via the standard AWS credential chain (env, shared config, IAM role, etc.).
type bedrockProvider struct {
	client bedrockConverseAPI
	logger *slog.Logger
}

// newBedrockProvider builds a bedrockProvider. Credentials are resolved through
// the default AWS credential chain; only the region comes from Logchef config.
func newBedrockProvider(ctx context.Context, cfg ProviderConfig, logger *slog.Logger) (*bedrockProvider, error) {
	if cfg.Region == "" {
		return nil, fmt.Errorf("AWS region is required for the bedrock provider (set ai.region)")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config for bedrock: %w", err)
	}

	logger.Debug("using AWS Bedrock provider", "region", cfg.Region)

	return &bedrockProvider{
		client: bedrockruntime.NewFromConfig(awsCfg),
		logger: logger.With("component", "bedrock_provider"),
	}, nil
}

// Name implements Provider.
func (p *bedrockProvider) Name() string { return ProviderBedrock }

// Complete implements Provider using the Bedrock Converse API.
func (p *bedrockProvider) Complete(ctx context.Context, req CompletionRequest) (string, error) {
	model := req.Model

	// MaxTokens is a small, bounded config value; clamp to the int32 range the
	// Converse API expects.
	maxTokens := req.MaxTokens
	if maxTokens < 0 {
		maxTokens = 0
	}
	if maxTokens > math.MaxInt32 {
		maxTokens = math.MaxInt32
	}

	out, err := p.client.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId: &model,
		System: []types.SystemContentBlock{
			&types.SystemContentBlockMemberText{Value: req.System},
		},
		Messages: []types.Message{
			{
				Role: types.ConversationRoleUser,
				Content: []types.ContentBlock{
					&types.ContentBlockMemberText{Value: req.User},
				},
			},
		},
		InferenceConfig: &types.InferenceConfiguration{
			MaxTokens:   aws.Int32(int32(maxTokens)),
			Temperature: aws.Float32(req.Temperature),
		},
	})
	if err != nil {
		return "", fmt.Errorf("bedrock Converse API error: %w", err)
	}

	msg, ok := out.Output.(*types.ConverseOutputMemberMessage)
	if !ok || msg == nil {
		return "", fmt.Errorf("no message in Bedrock Converse response")
	}

	var sb strings.Builder
	for _, block := range msg.Value.Content {
		if text, ok := block.(*types.ContentBlockMemberText); ok {
			sb.WriteString(text.Value)
		}
	}

	if sb.Len() == 0 {
		return "", fmt.Errorf("no text content in Bedrock Converse response")
	}

	return sb.String(), nil
}
