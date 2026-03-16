/*
Copyright (c) 2026 OpenInfra Foundation Europe. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package nftables

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewManager(t *testing.T) {
	mgr, err := NewManager(0, 4)
	assert.NoError(t, err)
	assert.NotNil(t, mgr)
	assert.Equal(t, "meridio-lb", mgr.tableName)
}

func TestExtractVIPs(t *testing.T) {
	tests := []struct {
		name     string
		cidrs    []string
		expected []string
	}{
		{
			name:     "single IPv4",
			cidrs:    []string{"192.168.1.1/32"},
			expected: []string{"192.168.1.1/32"},
		},
		{
			name:     "single IPv6",
			cidrs:    []string{"2001:db8::1/128"},
			expected: []string{"2001:db8::1/128"},
		},
		{
			name:     "mixed IPv4 and IPv6",
			cidrs:    []string{"192.168.1.1/32", "2001:db8::1/128"},
			expected: []string{"192.168.1.1/32", "2001:db8::1/128"},
		},
		{
			name:     "duplicates removed",
			cidrs:    []string{"192.168.1.1/32", "192.168.1.1/32"},
			expected: []string{"192.168.1.1/32"},
		},
		{
			name:     "empty list",
			cidrs:    []string{},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deduplicateVIPs(tt.cidrs)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

func TestSplitIPv4AndIPv6(t *testing.T) {
	tests := []struct {
		name         string
		cidrs        []string
		expectedIPv4 []string
		expectedIPv6 []string
	}{
		{
			name:         "only IPv4",
			cidrs:        []string{"192.168.1.1/32", "10.0.0.1/32"},
			expectedIPv4: []string{"192.168.1.1/32", "10.0.0.1/32"},
			expectedIPv6: []string{},
		},
		{
			name:         "only IPv6",
			cidrs:        []string{"2001:db8::1/128", "2001:db8::2/128"},
			expectedIPv4: []string{},
			expectedIPv6: []string{"2001:db8::1/128", "2001:db8::2/128"},
		},
		{
			name:         "mixed",
			cidrs:        []string{"192.168.1.1/32", "2001:db8::1/128"},
			expectedIPv4: []string{"192.168.1.1/32"},
			expectedIPv6: []string{"2001:db8::1/128"},
		},
		{
			name:         "empty",
			cidrs:        []string{},
			expectedIPv4: []string{},
			expectedIPv6: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ipv4, ipv6 := splitIPv4AndIPv6(tt.cidrs)
			assert.ElementsMatch(t, tt.expectedIPv4, ipv4)
			assert.ElementsMatch(t, tt.expectedIPv6, ipv6)
		})
	}
}
