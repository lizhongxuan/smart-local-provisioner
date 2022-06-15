package trylock

import (
	"testing"
)

func Test_TryLock(t *testing.T) {
	tests := []struct {
		name   string
		fields *TryLock
		want   bool
	}{
		{"test1",Build(),true},
	}

		for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tl := &TryLock{
				ch: tt.fields.ch,
			}
			if got := tl.TryLock(); got != tt.want {
				t.Errorf("TryLock() = %v, want %v", got, tt.want)
			}
			if got := tl.TryUnLock(); got != tt.want {
				t.Errorf("TryUnLock() = %v, want %v", got, tt.want)
			}

			if got := tl.TryLock(); got != tt.want {
				t.Errorf("TryLock() = %v, want %v", got, tt.want)
			}
			if got := tl.TryLock(); got == tt.want {
				t.Errorf("Second TryLock() = %v, want %v", got, tt.want)
			}
		})
	}
}