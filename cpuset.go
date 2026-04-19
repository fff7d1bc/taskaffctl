package main

import (
	"fmt"
	"math/bits"
	"sort"
	"strconv"
	"strings"
)

type CPUSet struct {
	words []uint64
}

func NewCPUSet() CPUSet {
	return CPUSet{}
}

func NewCPUSetSized(maxCPU int) CPUSet {
	if maxCPU < 0 {
		return NewCPUSet()
	}
	return CPUSet{words: make([]uint64, maxCPU/64+1)}
}

func ParseCPUSet(s string) (CPUSet, error) {
	set := NewCPUSet()
	s = strings.TrimSpace(s)
	if s == "" {
		return set, nil
	}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			start, err := strconv.Atoi(bounds[0])
			if err != nil {
				return CPUSet{}, fmt.Errorf("invalid CPU range start %q: %w", bounds[0], err)
			}
			end, err := strconv.Atoi(bounds[1])
			if err != nil {
				return CPUSet{}, fmt.Errorf("invalid CPU range end %q: %w", bounds[1], err)
			}
			if end < start || start < 0 {
				return CPUSet{}, fmt.Errorf("invalid CPU range %q", part)
			}
			for cpu := start; cpu <= end; cpu++ {
				set.Add(cpu)
			}
			continue
		}
		cpu, err := strconv.Atoi(part)
		if err != nil || cpu < 0 {
			return CPUSet{}, fmt.Errorf("invalid CPU %q", part)
		}
		set.Add(cpu)
	}
	return set, nil
}

func (s *CPUSet) ensure(cpu int) {
	idx := cpu / 64
	if idx < len(s.words) {
		return
	}
	words := make([]uint64, idx+1)
	copy(words, s.words)
	s.words = words
}

func (s *CPUSet) Add(cpu int) {
	if cpu < 0 {
		return
	}
	s.ensure(cpu)
	s.words[cpu/64] |= 1 << (cpu % 64)
}

func (s *CPUSet) Remove(cpu int) {
	idx := cpu / 64
	if cpu < 0 || idx >= len(s.words) {
		return
	}
	s.words[idx] &^= 1 << (cpu % 64)
}

func (s CPUSet) Has(cpu int) bool {
	idx := cpu / 64
	if cpu < 0 || idx >= len(s.words) {
		return false
	}
	return s.words[idx]&(1<<(cpu%64)) != 0
}

func (s CPUSet) Clone() CPUSet {
	out := CPUSet{words: make([]uint64, len(s.words))}
	copy(out.words, s.words)
	return out
}

func (s CPUSet) Intersect(other CPUSet) CPUSet {
	n := len(s.words)
	if len(other.words) < n {
		n = len(other.words)
	}
	out := CPUSet{words: make([]uint64, n)}
	for i := 0; i < n; i++ {
		out.words[i] = s.words[i] & other.words[i]
	}
	return out.trimmed()
}

func (s CPUSet) Difference(other CPUSet) CPUSet {
	out := CPUSet{words: make([]uint64, len(s.words))}
	for i := range s.words {
		word := s.words[i]
		if i < len(other.words) {
			word &^= other.words[i]
		}
		out.words[i] = word
	}
	return out.trimmed()
}

func (s CPUSet) Equal(other CPUSet) bool {
	a := s.trimmed()
	b := other.trimmed()
	if len(a.words) != len(b.words) {
		return false
	}
	for i := range a.words {
		if a.words[i] != b.words[i] {
			return false
		}
	}
	return true
}

func (s CPUSet) IsEmpty() bool {
	for _, word := range s.words {
		if word != 0 {
			return false
		}
	}
	return true
}

func (s CPUSet) Count() int {
	total := 0
	for _, word := range s.words {
		total += bits.OnesCount64(word)
	}
	return total
}

func (s CPUSet) CPUs() []int {
	var cpus []int
	for wi, word := range s.words {
		for word != 0 {
			bit := bits.TrailingZeros64(word)
			cpus = append(cpus, wi*64+bit)
			word &^= 1 << bit
		}
	}
	return cpus
}

func (s CPUSet) MaxCPU() int {
	for i := len(s.words) - 1; i >= 0; i-- {
		if s.words[i] == 0 {
			continue
		}
		return i*64 + 63 - bits.LeadingZeros64(s.words[i])
	}
	return -1
}

func (s CPUSet) String() string {
	cpus := s.CPUs()
	if len(cpus) == 0 {
		return ""
	}
	sort.Ints(cpus)
	var parts []string
	for i := 0; i < len(cpus); {
		start := cpus[i]
		end := start
		for i+1 < len(cpus) && cpus[i+1] == end+1 {
			i++
			end = cpus[i]
		}
		if start == end {
			parts = append(parts, strconv.Itoa(start))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", start, end))
		}
		i++
	}
	return strings.Join(parts, ",")
}

func (s CPUSet) trimmed() CPUSet {
	n := len(s.words)
	for n > 0 && s.words[n-1] == 0 {
		n--
	}
	out := CPUSet{words: make([]uint64, n)}
	copy(out.words, s.words[:n])
	return out
}

func (s CPUSet) toBytes() []byte {
	if len(s.words) == 0 {
		return make([]byte, 8)
	}
	out := make([]byte, len(s.words)*8)
	for i, word := range s.words {
		base := i * 8
		out[base+0] = byte(word)
		out[base+1] = byte(word >> 8)
		out[base+2] = byte(word >> 16)
		out[base+3] = byte(word >> 24)
		out[base+4] = byte(word >> 32)
		out[base+5] = byte(word >> 40)
		out[base+6] = byte(word >> 48)
		out[base+7] = byte(word >> 56)
	}
	return out
}

func cpuSetFromBytes(buf []byte) CPUSet {
	words := make([]uint64, (len(buf)+7)/8)
	for i := range words {
		base := i * 8
		var word uint64
		for j := 0; j < 8 && base+j < len(buf); j++ {
			word |= uint64(buf[base+j]) << (8 * j)
		}
		words[i] = word
	}
	return CPUSet{words: words}.trimmed()
}
