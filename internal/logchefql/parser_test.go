package logchefql

import (
    "testing"

    "github.com/stretchr/testify/require"
)

func TestParser(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        wantErr  bool
        validate func(*testing.T, *Query)
    }{
        {
            name:  "simple equality",
            input: "service_name='api'",
            validate: func(t *testing.T, q *Query) {
                require.Len(t, q.Filters, 1)
                f := q.Filters[0]
                require.Equal(t, "service_name", f.Field.Name)
                require.Equal(t, "=", f.Operator)
                require.Equal(t, "api", f.Value.GetValue())
            },
        },
        {
            name:  "multiple filters",
            input: "service_name='api';severity_text='error'",
            validate: func(t *testing.T, q *Query) {
                require.Len(t, q.Filters, 2)
                require.Equal(t, "service_name", q.Filters[0].Field.Name)
                require.Equal(t, "severity_text", q.Filters[1].Field.Name)
            },
        },
        {
            name:  "nested field access", 
            input: "p.error.code='500'",
            validate: func(t *testing.T, q *Query) {
                require.Len(t, q.Filters, 1)
                f := q.Filters[0]
                require.Equal(t, "p", f.Field.Name)
                require.Equal(t, []string{"error", "code"}, f.Field.SubFields)
            },
        },
        {
            name:  "relative time",
            input: "timestamp=-1h",
            validate: func(t *testing.T, q *Query) {
                require.Len(t, q.Filters, 1)
                f := q.Filters[0]
                require.Equal(t, "timestamp", f.Field.Name)
                require.Equal(t, "=", f.Operator)
                require.Equal(t, "-1h", f.Value.GetValue())
            },
        },
        {
            name: "complex service error scenario",
            input: "service_name='payment-service';p.error.retry_count>3;body~'timeout';timestamp>-1h",
            validate: func(t *testing.T, q *Query) {
                require.Len(t, q.Filters, 4)
                // Service name check
                require.Equal(t, "service_name", q.Filters[0].Field.Name)
                require.Equal(t, "=", q.Filters[0].Operator)
                require.Equal(t, "payment-service", q.Filters[0].Value.GetValue())
                // Retry count check
                require.Equal(t, "p", q.Filters[1].Field.Name)
                require.Equal(t, []string{"error", "retry_count"}, q.Filters[1].Field.SubFields)
                require.Equal(t, ">", q.Filters[1].Operator)
                require.Equal(t, "3", q.Filters[1].Value.GetValue())
                // Body pattern check
                require.Equal(t, "body", q.Filters[2].Field.Name)
                require.Equal(t, "~", q.Filters[2].Operator)
                require.Equal(t, "timeout", q.Filters[2].Value.GetValue())
                // Timestamp check
                require.Equal(t, "timestamp", q.Filters[3].Field.Name)
                require.Equal(t, ">", q.Filters[3].Operator)
                require.Equal(t, "-1h", q.Filters[3].Value.GetValue())
            },
        },
        {
            name: "complex service monitoring",
            input: "service_name='api-gateway';status_code>=500;p.response.latency>1000;p.error.message~'timeout';timestamp>-15m",
            validate: func(t *testing.T, q *Query) {
                require.Len(t, q.Filters, 5)
                // Service name check
                require.Equal(t, "service_name", q.Filters[0].Field.Name)
                require.Equal(t, "=", q.Filters[0].Operator)
                require.Equal(t, "api-gateway", q.Filters[0].Value.GetValue())
                // Status code check
                require.Equal(t, "status_code", q.Filters[1].Field.Name)
                require.Equal(t, ">=", q.Filters[1].Operator)
                require.Equal(t, "500", q.Filters[1].Value.GetValue())
                // Response latency check
                require.Equal(t, "p", q.Filters[2].Field.Name)
                require.Equal(t, []string{"response", "latency"}, q.Filters[2].Field.SubFields)
                require.Equal(t, ">", q.Filters[2].Operator)
                require.Equal(t, "1000", q.Filters[2].Value.GetValue())
                // Error message pattern check
                require.Equal(t, "p", q.Filters[3].Field.Name)
                require.Equal(t, []string{"error", "message"}, q.Filters[3].Field.SubFields)
                require.Equal(t, "~", q.Filters[3].Operator)
                require.Equal(t, "timeout", q.Filters[3].Value.GetValue())
                // Time window check
                require.Equal(t, "timestamp", q.Filters[4].Field.Name)
                require.Equal(t, ">", q.Filters[4].Operator)
                require.Equal(t, "-15m", *q.Filters[4].Value.RelativeTime)
            },
        },
        {
            name: "database error monitoring",
            input: "service='db-proxy';p.query.duration>1000;severity_text='error';p.error.type~'deadlock';timestamp>-5m",
            validate: func(t *testing.T, q *Query) {
                require.Len(t, q.Filters, 5)
                // Service check
                require.Equal(t, "service", q.Filters[0].Field.Name)
                require.Equal(t, "=", q.Filters[0].Operator)
                require.Equal(t, "db-proxy", *q.Filters[0].Value.String)
                // Query duration check
                require.Equal(t, "p", q.Filters[1].Field.Name)
                require.Equal(t, []string{"query", "duration"}, q.Filters[1].Field.SubFields)
                require.Equal(t, ">", q.Filters[1].Operator)
                require.Equal(t, "1000", q.Filters[1].Value.GetValue())
                // Severity check
                require.Equal(t, "severity_text", q.Filters[2].Field.Name)
                require.Equal(t, "=", q.Filters[2].Operator)
                require.Equal(t, "error", *q.Filters[2].Value.String)
                // Error type pattern check
                require.Equal(t, "p", q.Filters[3].Field.Name)
                require.Equal(t, []string{"error", "type"}, q.Filters[3].Field.SubFields)
                require.Equal(t, "~", q.Filters[3].Operator)
                require.Equal(t, "deadlock", *q.Filters[3].Value.String)
                // Time window check
                require.Equal(t, "timestamp", q.Filters[4].Field.Name)
                require.Equal(t, ">", q.Filters[4].Operator)
                require.Equal(t, "-5m", *q.Filters[4].Value.RelativeTime)
            },
        },
        {
            name: "api gateway monitoring",
            input: "service='api-gateway';status_code!=200;p.response.latency>500;p.client.ip~'10.0.';timestamp>-15m",
            validate: func(t *testing.T, q *Query) {
                require.Len(t, q.Filters, 5)
                // Service check
                require.Equal(t, "service", q.Filters[0].Field.Name)
                require.Equal(t, "=", q.Filters[0].Operator)
                require.Equal(t, "api-gateway", *q.Filters[0].Value.String)
                // Status code check
                require.Equal(t, "status_code", q.Filters[1].Field.Name)
                require.Equal(t, "!=", q.Filters[1].Operator)
                require.Equal(t, "200", q.Filters[1].Value.GetValue())
                // Response latency check
                require.Equal(t, "p", q.Filters[2].Field.Name)
                require.Equal(t, []string{"response", "latency"}, q.Filters[2].Field.SubFields)
                require.Equal(t, ">", q.Filters[2].Operator)
                require.Equal(t, "500", q.Filters[2].Value.GetValue())
                // Client IP pattern check
                require.Equal(t, "p", q.Filters[3].Field.Name)
                require.Equal(t, []string{"client", "ip"}, q.Filters[3].Field.SubFields)
                require.Equal(t, "~", q.Filters[3].Operator)
                require.Equal(t, "10.0.", *q.Filters[3].Value.String)
                // Time window check
                require.Equal(t, "timestamp", q.Filters[4].Field.Name)
                require.Equal(t, ">", q.Filters[4].Operator)
                require.Equal(t, "-15m", *q.Filters[4].Value.RelativeTime)
            },
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            ast, err := Parse(tt.input)
            if tt.wantErr {
                require.Error(t, err)
                return
            }
            require.NoError(t, err)
            if tt.validate != nil {
                tt.validate(t, ast)
            }
        })
    }
}
