package models

import "fmt"

// MaxHistogramBuckets matches the frontend's sparse histogram fill budget.
const MaxHistogramBuckets = 5000

// MaxHistogramResponseBytes bounds buffered histogram data before serialization.
const MaxHistogramResponseBytes = 16 * 1024 * 1024

// ErrHistogramBudgetExceeded rejects incomplete histograms so totals stay exact.
var ErrHistogramBudgetExceeded = fmt.Errorf("histogram exceeds the limit of %d time buckets or %d MiB. Use a shorter time range, a larger interval, or a different grouping field", MaxHistogramBuckets, MaxHistogramResponseBytes/(1024*1024))
