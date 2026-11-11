package domain_test

import (
	"errors"
	"testing"

	"github.com/SebastianP07/wallet-track-core-app/internal/periods/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

func TestNewOpeningBalance(t *testing.T) {
	value := func(v int64) *int64 { return &v }

	tests := []struct {
		name    string
		raw     *int64
		wantErr bool
	}{
		{name: "nil clears it", raw: nil},
		{name: "zero", raw: value(0)},
		{name: "positive", raw: value(24_165_723)},
		{name: "negative, like Total actual", raw: value(-5_000)},
		{name: "the upper bound", raw: value(int64(kernel.MaxMoney))},
		{name: "the lower bound", raw: value(-int64(kernel.MaxMoney))},
		{name: "past the upper bound", raw: value(int64(kernel.MaxMoney) + 1), wantErr: true},
		{name: "past the lower bound", raw: value(-int64(kernel.MaxMoney) - 1), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.NewOpeningBalance(tt.raw)

			if tt.wantErr {
				var invalid *domain.ValidationError
				if !errors.As(err, &invalid) || len(invalid.Violations) != 1 || invalid.Violations[0].Field != "openingBalance" {
					t.Fatalf("err = %v, want one openingBalance violation", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if tt.raw == nil {
				if got != nil {
					t.Errorf("got %d, want nil", *got)
				}
				return
			}
			if got == nil || int64(*got) != *tt.raw {
				t.Errorf("got %v, want %d", got, *tt.raw)
			}
		})
	}
}
