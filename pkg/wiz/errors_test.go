package wiz

import (
	"errors"
	"testing"
)

func TestWizError(t *testing.T) {
	t.Run("NewError creates error with type", func(t *testing.T) {
		inner := errors.New("connection refused")
		wizErr := NewError("offline", inner)

		if wizErr.Type != "offline" {
			t.Errorf("expected type 'offline', got %q", wizErr.Type)
		}

		if wizErr.Err != inner {
			t.Error("expected inner error to be preserved")
		}
	})

	t.Run("ErrorType extracts type from WizError", func(t *testing.T) {
		wizErr := NewError("timeout", errors.New("timed out"))

		if got := ErrorType(wizErr); got != "timeout" {
			t.Errorf("expected type 'timeout', got %q", got)
		}
	})

	t.Run("ErrorType returns empty for non-WizError", func(t *testing.T) {
		normalErr := errors.New("normal error")

		if got := ErrorType(normalErr); got != "" {
			t.Errorf("expected empty type, got %q", got)
		}
	})

	t.Run("IsDeviceOffline returns true for offline errors", func(t *testing.T) {
		wizErr := NewError("offline", ErrDeviceOffline)

		if !IsDeviceOffline(wizErr) {
			t.Error("expected IsDeviceOffline to return true")
		}
	})

	t.Run("IsDeviceOffline returns false for other errors", func(t *testing.T) {
		wizErr := NewError("timeout", ErrTimeout)

		if IsDeviceOffline(wizErr) {
			t.Error("expected IsDeviceOffline to return false for timeout error")
		}
	})

	t.Run("IsTimeout returns true for timeout errors", func(t *testing.T) {
		wizErr := NewError("timeout", ErrTimeout)

		if !IsTimeout(wizErr) {
			t.Error("expected IsTimeout to return true")
		}
	})

	t.Run("IsUnauthorized returns true for unauthorized errors", func(t *testing.T) {
		wizErr := NewError("unauthorized", ErrUnauthorized)

		if !IsUnauthorized(wizErr) {
			t.Error("expected IsUnauthorized to return true")
		}
	})

	t.Run("Error message uses Message field if set", func(t *testing.T) {
		wizErr := &WizError{
			Type:    "offline",
			Message: "Device is not responding",
			Err:     errors.New("connection refused"),
		}

		if got := wizErr.Error(); got != "Device is not responding" {
			t.Errorf("expected 'Device is not responding', got %q", got)
		}
	})

	t.Run("Error message falls back to inner error", func(t *testing.T) {
		inner := errors.New("connection refused")
		wizErr := NewError("offline", inner)

		if got := wizErr.Error(); got != "connection refused" {
			t.Errorf("expected 'connection refused', got %q", got)
		}
	})

	t.Run("Error message returns default for empty error", func(t *testing.T) {
		wizErr := &WizError{Type: "unknown"}

		if got := wizErr.Error(); got != "unknown wiz error" {
			t.Errorf("expected 'unknown wiz error', got %q", got)
		}
	})

	t.Run("Unwrap returns inner error", func(t *testing.T) {
		inner := errors.New("connection refused")
		wizErr := NewError("offline", inner)

		if got := wizErr.Unwrap(); got != inner {
			t.Error("expected Unwrap to return inner error")
		}
	})
}

func TestPredefinedErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"ErrDeviceOffline", ErrDeviceOffline, "device is offline"},
		{"ErrUnauthorized", ErrUnauthorized, "authentication failed"},
		{"ErrTimeout", ErrTimeout, "request timed out"},
		{"ErrNetworkFailure", ErrNetworkFailure, "network failure"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}
