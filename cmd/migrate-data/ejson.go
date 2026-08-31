package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Wrapper types that decode Compass's canonical Extended JSON forms (and a
// bare-value fallback) via encoding/json.

// oid is a MongoDB ObjectId: {"$oid":"<24 lowercase hex>"} or a bare string.
type oid string

func (o *oid) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	var w struct {
		OID string `json:"$oid"`
	}
	if err := json.Unmarshal(b, &w); err == nil && w.OID != "" {
		return o.set(w.OID)
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("ejson oid: %s: %w", b, err)
	}
	return o.set(s)
}

func (o *oid) set(s string) error {
	if !isObjectID(s) {
		return fmt.Errorf("ejson oid: %q is not 24 lowercase hex chars", s)
	}
	*o = oid(s)
	return nil
}

func isObjectID(s string) bool {
	if len(s) != 24 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ejsonDate is a Compass date: {"$date":"<iso>"} or
// {"$date":{"$numberLong":"<unix millis>"}} or a bare iso string.
type ejsonDate struct{ time.Time }

func (d *ejsonDate) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	var wrap struct {
		Date json.RawMessage `json:"$date"`
	}
	if err := json.Unmarshal(b, &wrap); err == nil && len(wrap.Date) > 0 {
		return d.parseValue(wrap.Date)
	}
	return d.parseValue(b)
}

func (d *ejsonDate) parseValue(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return fmt.Errorf("ejson date: %w", err)
		}
		d.Time = t
		return nil
	}
	var ms struct {
		N string `json:"$numberLong"`
	}
	if err := json.Unmarshal(b, &ms); err == nil && ms.N != "" {
		n, err := strconv.ParseInt(ms.N, 10, 64)
		if err != nil {
			return fmt.Errorf("ejson date millis: %w", err)
		}
		d.Time = time.UnixMilli(n).UTC()
		return nil
	}
	return fmt.Errorf("ejson date: unrecognised form %s", b)
}

// ejsonInt is a Compass integer: {"$numberLong":"<n>"} / {"$numberInt":"<n>"}
// or a bare JSON number.
type ejsonInt int64

func (n *ejsonInt) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	var raw int64
	if err := json.Unmarshal(b, &raw); err == nil {
		*n = ejsonInt(raw)
		return nil
	}
	var w struct {
		L string `json:"$numberLong"`
		I string `json:"$numberInt"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return fmt.Errorf("ejson int: %s: %w", b, err)
	}
	v, err := strconv.ParseInt(firstNonEmpty(w.L, w.I), 10, 64)
	if err != nil {
		return fmt.Errorf("ejson int: %s: %w", b, err)
	}
	*n = ejsonInt(v)
	return nil
}

// ejsonFloat is a Compass number: {"$numberDouble":"<n>"} / {"$numberLong":"<n>"}
// / {"$numberInt":"<n>"} or a bare JSON number.
type ejsonFloat float64

func (n *ejsonFloat) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	var raw float64
	if err := json.Unmarshal(b, &raw); err == nil {
		*n = ejsonFloat(raw)
		return nil
	}
	var w struct {
		D string `json:"$numberDouble"`
		L string `json:"$numberLong"`
		I string `json:"$numberInt"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return fmt.Errorf("ejson number: %s: %w", b, err)
	}
	v, err := strconv.ParseFloat(firstNonEmpty(w.D, w.L, w.I), 64)
	if err != nil {
		return fmt.Errorf("ejson number: %s: %w", b, err)
	}
	*n = ejsonFloat(v)
	return nil
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
