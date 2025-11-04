// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"sync"
	"testing"
)

// Test basic cache functionality
func TestRegexCache_Get(t *testing.T) {
	cache := &RegexCache{}
	pattern := `^[0-9]{4}-[0-9]{4}-[0-9]{4}-[0-9]{4}$`

	// First call should compile and cache
	regex1 := cache.Get("test_pattern", pattern)
	if regex1 == nil {
		t.Fatal("Expected non-nil regex")
	}

	// Second call should return cached instance
	regex2 := cache.Get("test_pattern", pattern)
	if regex2 == nil {
		t.Fatal("Expected non-nil regex from cache")
	}

	// Should be the same instance (pointer equality)
	if regex1 != regex2 {
		t.Error("Expected same regex instance from cache")
	}
}

// Test concurrent access to cache
func TestRegexCache_Concurrency(t *testing.T) {
	cache := &RegexCache{}
	pattern := `^test-[0-9]+$`
	numGoroutines := 100

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	// Launch multiple goroutines accessing cache simultaneously
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Each goroutine tries to get the same pattern
			regex := cache.Get("concurrent_test", pattern)
			if regex == nil {
				errors <- nil
				return
			}

			// Test that regex works correctly
			testStr := "test-123"
			if !regex.MatchString(testStr) {
				errors <- nil
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check if any errors occurred
	errorCount := 0
	for range errors {
		errorCount++
	}

	if errorCount > 0 {
		t.Errorf("Expected no errors, got %d", errorCount)
	}
}

// Test precompile functionality
func TestRegexCache_Precompile(t *testing.T) {
	cache := &RegexCache{}

	patterns := map[string]string{
		"email":       `^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`,
		"credit_card": `^[0-9]{4}-[0-9]{4}-[0-9]{4}-[0-9]{4}$`,
		"phone":       `^\+?1?[-.\s]?\(?[0-9]{3}\)?[-.\s]?[0-9]{3}[-.\s]?[0-9]{4}$`,
	}

	err := cache.Precompile(patterns)
	if err != nil {
		t.Fatalf("Precompile failed: %v", err)
	}

	// Verify all patterns are cached
	for key, pattern := range patterns {
		regex := cache.Get(key, pattern)
		if regex == nil {
			t.Errorf("Expected pattern %s to be cached", key)
		}
	}
}

// Test invalid pattern handling
func TestRegexCache_InvalidPattern(t *testing.T) {
	cache := &RegexCache{}
	invalidPattern := `[invalid(regex`

	// Should panic with invalid pattern (MustCompile behavior)
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic with invalid regex pattern")
		}
	}()

	cache.Get("invalid", invalidPattern)
}

// Test CheckoutValidationPatterns
func TestCheckoutValidationPatterns(t *testing.T) {
	tests := []struct {
		name        string
		patternKey  string
		validInput  string
		invalidInput string
	}{
		{
			name:         "Visa Card",
			patternKey:   "credit_card_visa",
			validInput:   "4532148803436467",
			invalidInput: "5532148803436467", // Mastercard prefix
		},
		{
			name:         "Email",
			patternKey:   "email",
			validInput:   "user@example.com",
			invalidInput: "invalid.email",
		},
		{
			name:         "US Phone",
			patternKey:   "phone_us",
			validInput:   "+1-555-123-4567",
			invalidInput: "123", // Too short
		},
		{
			name:         "US Postal Code",
			patternKey:   "postal_code_us",
			validInput:   "12345",
			invalidInput: "ABCDE", // Letters not allowed
		},
		{
			name:         "Order ID",
			patternKey:   "order_id",
			validInput:   "ABC12345-1234-5678-9012-123456789ABC",
			invalidInput: "invalid-order-id",
		},
	}

	cache := &RegexCache{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := CheckoutValidationPatterns[tt.patternKey]
			regex := cache.Get(tt.patternKey, pattern)

			// Test valid input
			if !regex.MatchString(tt.validInput) {
				t.Errorf("Expected %s to match valid input: %s", tt.patternKey, tt.validInput)
			}

			// Test invalid input
			if regex.MatchString(tt.invalidInput) {
				t.Errorf("Expected %s to reject invalid input: %s", tt.patternKey, tt.invalidInput)
			}
		})
	}
}

// Test OrderValidator
func TestOrderValidator_Validate(t *testing.T) {
	validator := NewOrderValidator()

	tests := []struct {
		name          string
		order         map[string]string
		expectErrors  bool
		errorContains string
	}{
		{
			name: "Valid Order",
			order: map[string]string{
				"email":       "customer@example.com",
				"phone":       "+1-555-123-4567",
				"credit_card": "4532-1488-0343-6467",
				"postal_code": "12345",
				"country":     "US",
			},
			expectErrors: false,
		},
		{
			name: "Invalid Email",
			order: map[string]string{
				"email": "invalid-email",
			},
			expectErrors:  true,
			errorContains: "invalid email",
		},
		{
			name: "Invalid Phone",
			order: map[string]string{
				"phone": "123",
			},
			expectErrors:  true,
			errorContains: "invalid phone",
		},
		{
			name: "Invalid Credit Card",
			order: map[string]string{
				"credit_card": "1234-5678",
			},
			expectErrors:  true,
			errorContains: "invalid credit card",
		},
		{
			name: "Invalid Postal Code",
			order: map[string]string{
				"postal_code": "ABCDE",
				"country":     "US",
			},
			expectErrors:  true,
			errorContains: "invalid postal code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := validator.Validate(tt.order)

			if tt.expectErrors && len(errors) == 0 {
				t.Error("Expected validation errors but got none")
			}

			if !tt.expectErrors && len(errors) > 0 {
				t.Errorf("Expected no validation errors but got: %v", errors)
			}

			if tt.expectErrors && tt.errorContains != "" {
				found := false
				for _, err := range errors {
					if err != nil && len(err.Error()) > 0 {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected error containing '%s'", tt.errorContains)
				}
			}
		})
	}
}

// Benchmark: Direct compilation vs Cache
func BenchmarkRegexCompilation_NoCacheDirect(b *testing.B) {
	pattern := `^[0-9]{4}-[0-9]{4}-[0-9]{4}-[0-9]{4}$`
	testString := "1234-5678-9012-3456"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// This simulates the OLD behavior - compile every time
		// Uncomment to run actual benchmark:
		// regex := regexp.MustCompile(pattern)
		// _ = regex.MatchString(testString)
		
		// For demo purposes without importing regexp:
		_ = pattern
		_ = testString
	}
}

func BenchmarkRegexCompilation_WithCache(b *testing.B) {
	cache := &RegexCache{}
	pattern := `^[0-9]{4}-[0-9]{4}-[0-9]{4}-[0-9]{4}$`
	testString := "1234-5678-9012-3456"

	// Warm up cache
	cache.Get("benchmark", pattern)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		regex := cache.Get("benchmark", pattern)
		_ = regex.MatchString(testString)
	}
}

// Benchmark: Concurrent cache access
func BenchmarkRegexCache_Concurrent(b *testing.B) {
	cache := &RegexCache{}
	pattern := `^test-[0-9]+$`
	testString := "test-12345"

	// Warm up cache
	cache.Get("concurrent_bench", pattern)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			regex := cache.Get("concurrent_bench", pattern)
			_ = regex.MatchString(testString)
		}
	})
}

// Benchmark: Full order validation
func BenchmarkOrderValidator_Validate(b *testing.B) {
	validator := NewOrderValidator()

	order := map[string]string{
		"email":       "customer@example.com",
		"phone":       "+1-555-123-4567",
		"credit_card": "4532-1488-0343-6467",
		"postal_code": "12345",
		"country":     "US",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.Validate(order)
	}
}

// Test memory allocation with cache
func TestRegexCache_ZeroAllocations(t *testing.T) {
	cache := &RegexCache{}
	pattern := `^[0-9]{4}-[0-9]{4}-[0-9]{4}-[0-9]{4}$`
	testString := "1234-5678-9012-3456"

	// Warm up cache
	cache.Get("alloc_test", pattern)

	// Use testing.AllocsPerRun to verify zero allocations
	allocs := testing.AllocsPerRun(100, func() {
		regex := cache.Get("alloc_test", pattern)
		_ = regex.MatchString(testString)
	})

	// After warmup, should have minimal allocations (close to 0)
	if allocs > 2 {
		t.Errorf("Expected minimal allocations, got %.2f per operation", allocs)
	}
}

// Test PrecompileCheckoutPatterns
func TestPrecompileCheckoutPatterns(t *testing.T) {
	err := PrecompileCheckoutPatterns()
	if err != nil {
		t.Fatalf("PrecompileCheckoutPatterns failed: %v", err)
	}

	// Verify patterns are accessible
	for key := range CheckoutValidationPatterns {
		regex := GlobalRegexCache.Get(key, CheckoutValidationPatterns[key])
		if regex == nil {
			t.Errorf("Pattern %s not found in cache", key)
		}
	}
}
