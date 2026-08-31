package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestOIDUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    oid
		wantErr bool
	}{
		{"wrapped", `{"$oid":"6310ad7242687f4a1cf7f226"}`, "6310ad7242687f4a1cf7f226", false},
		{"bare string", `"6310ad7242687f4a1cf7f226"`, "6310ad7242687f4a1cf7f226", false},
		{"not 24 hex", `{"$oid":"abc"}`, "", true},
		{"uppercase rejected", `{"$oid":"6310AD7242687F4A1CF7F226"}`, "", true},
		{"null", `null`, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got oid
			err := json.Unmarshal([]byte(tt.in), &got)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEjsonDateUnmarshal(t *testing.T) {
	want := time.Date(2025, 7, 13, 19, 52, 51, 841_000_000, time.UTC)
	tests := []struct {
		name string
		in   string
	}{
		{"wrapped iso", `{"$date":"2025-07-13T19:52:51.841Z"}`},
		{"wrapped millis", `{"$date":{"$numberLong":"1752436371841"}}`},
		{"bare iso", `"2025-07-13T19:52:51.841Z"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got ejsonDate
			if err := json.Unmarshal([]byte(tt.in), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !got.Equal(want) {
				t.Fatalf("got %v, want %v", got.Time, want)
			}
		})
	}
}

func TestEjsonDateNullAndGarbage(t *testing.T) {
	var got ejsonDate
	if err := json.Unmarshal([]byte(`null`), &got); err != nil {
		t.Fatalf("null: %v", err)
	}
	if !got.IsZero() {
		t.Fatalf("null should decode to the zero time, got %v", got.Time)
	}
	if err := json.Unmarshal([]byte(`{"$date":"not-a-date"}`), &got); err == nil {
		t.Fatal("expected an error for an unparseable date")
	}
}

func TestEjsonIntUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want ejsonInt
	}{
		{"numberLong", `{"$numberLong":"30"}`, 30},
		{"numberInt", `{"$numberInt":"4"}`, 4},
		{"bare", `360`, 360},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got ejsonInt
			if err := json.Unmarshal([]byte(tt.in), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEjsonFloatUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want ejsonFloat
	}{
		{"bare int", `2`, 2},
		{"bare float", `1.5`, 1.5},
		{"numberDouble", `{"$numberDouble":"0.5"}`, 0.5},
		{"numberLong", `{"$numberLong":"800"}`, 800},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got ejsonFloat
			if err := json.Unmarshal([]byte(tt.in), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEjsonWithinStruct(t *testing.T) {
	const doc = `{"itemId":{"$oid":"6310ad7242687f4a1cf7f240"},"unitId":{"$oid":"68738ad4d5730ccdb15ca13f"},"quantity":2}`
	var ing struct {
		ItemID   oid        `json:"itemId"`
		UnitID   oid        `json:"unitId"`
		Quantity ejsonFloat `json:"quantity"`
	}
	if err := json.Unmarshal([]byte(doc), &ing); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ing.ItemID != "6310ad7242687f4a1cf7f240" || ing.UnitID != "68738ad4d5730ccdb15ca13f" || ing.Quantity != 2 {
		t.Fatalf("got %+v", ing)
	}
}
