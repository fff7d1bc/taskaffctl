package main

import "testing"

func TestParseCPUSet(t *testing.T) {
	set, err := ParseCPUSet("0-3,8,10-11")
	if err != nil {
		t.Fatal(err)
	}
	if got := set.String(); got != "0-3,8,10-11" {
		t.Fatalf("unexpected cpuset string: %s", got)
	}
}

func TestCPUSetIntersect(t *testing.T) {
	a, _ := ParseCPUSet("0-7")
	b, _ := ParseCPUSet("4-11")
	if got := a.Intersect(b).String(); got != "4-7" {
		t.Fatalf("unexpected intersection: %s", got)
	}
}
