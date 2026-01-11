package daemon

import "testing"

func TestGetIntFromPayload(t *testing.T) {
	tests := []struct {
		name     string
		payload  map[string]interface{}
		key      string
		wantVal  int
		wantOk   bool
	}{
		{
			name:    "int value",
			payload: map[string]interface{}{"count": 42},
			key:     "count",
			wantVal: 42,
			wantOk:  true,
		},
		{
			name:    "float64 value",
			payload: map[string]interface{}{"count": float64(42)},
			key:     "count",
			wantVal: 42,
			wantOk:  true,
		},
		{
			name:    "int64 value",
			payload: map[string]interface{}{"count": int64(42)},
			key:     "count",
			wantVal: 42,
			wantOk:  true,
		},
		{
			name:    "missing key",
			payload: map[string]interface{}{"other": 42},
			key:     "count",
			wantVal: 0,
			wantOk:  false,
		},
		{
			name:    "wrong type (string)",
			payload: map[string]interface{}{"count": "42"},
			key:     "count",
			wantVal: 0,
			wantOk:  false,
		},
		{
			name:    "zero int",
			payload: map[string]interface{}{"count": 0},
			key:     "count",
			wantVal: 0,
			wantOk:  true,
		},
		{
			name:    "zero float64",
			payload: map[string]interface{}{"count": float64(0)},
			key:     "count",
			wantVal: 0,
			wantOk:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVal, gotOk := getIntFromPayload(tt.payload, tt.key)
			if gotVal != tt.wantVal {
				t.Errorf("getIntFromPayload() value = %v, want %v", gotVal, tt.wantVal)
			}
			if gotOk != tt.wantOk {
				t.Errorf("getIntFromPayload() ok = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}

func TestGetInt64FromPayload(t *testing.T) {
	tests := []struct {
		name     string
		payload  map[string]interface{}
		key      string
		wantVal  int64
		wantOk   bool
	}{
		{
			name:    "int value",
			payload: map[string]interface{}{"count": 42},
			key:     "count",
			wantVal: 42,
			wantOk:  true,
		},
		{
			name:    "int64 value",
			payload: map[string]interface{}{"count": int64(9223372036854775807)},
			key:     "count",
			wantVal: 9223372036854775807,
			wantOk:  true,
		},
		{
			name:    "float64 value",
			payload: map[string]interface{}{"count": float64(42)},
			key:     "count",
			wantVal: 42,
			wantOk:  true,
		},
		{
			name:    "missing key",
			payload: map[string]interface{}{"other": 42},
			key:     "count",
			wantVal: 0,
			wantOk:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVal, gotOk := getInt64FromPayload(tt.payload, tt.key)
			if gotVal != tt.wantVal {
				t.Errorf("getInt64FromPayload() value = %v, want %v", gotVal, tt.wantVal)
			}
			if gotOk != tt.wantOk {
				t.Errorf("getInt64FromPayload() ok = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}
