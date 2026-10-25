package draw

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func makeParticipants(n int) []string {
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = fmt.Sprintf("P%d", i+1)
	}
	return ids
}

func countCycles(assignments map[string]string) (numCycles int, minCycleLen int, maxCycleLen int) {
	visited := make(map[string]bool, len(assignments))
	minCycleLen = len(assignments) + 1
	for start := range assignments {
		if visited[start] {
			continue
		}
		numCycles++
		curr := start
		length := 0
		for !visited[curr] {
			visited[curr] = true
			curr = assignments[curr]
			length++
		}
		if length < minCycleLen {
			minCycleLen = length
		}
		if length > maxCycleLen {
			maxCycleLen = length
		}
	}
	return numCycles, minCycleLen, maxCycleLen
}

func TestSolveSingleHamiltonianCycleSizes(t *testing.T) {
	for _, n := range []int{3, 4, 5, 10, 50} {
		ids := makeParticipants(n)
		res, err := Solve(ids, nil)
		if err != nil {
			t.Fatalf("N=%d: unexpected error: %v", n, err)
		}
		if res.Mode != ModeSingleCycle {
			t.Fatalf("N=%d: expected ModeSingleCycle, got %s", n, res.Mode)
		}
		if len(res.Assignments) != n {
			t.Fatalf("N=%d: expected %d assignments, got %d", n, n, len(res.Assignments))
		}
		numCycles, minLen, maxLen := countCycles(res.Assignments)
		if numCycles != 1 || minLen != n || maxLen != n {
			t.Fatalf("N=%d: expected 1 cycle of length %d, got %d cycles (min=%d, max=%d)", n, n, numCycles, minLen, maxLen)
		}
		for a, b := range res.Assignments {
			if a == b {
				t.Fatalf("N=%d: self-assignment %s -> %s", n, a, b)
			}
			if res.Assignments[b] == a {
				t.Fatalf("N=%d: reciprocal assignment %s <-> %s", n, a, b)
			}
		}
	}
}

func TestSolveEquiprobabilityN4(t *testing.T) {
	ids := []string{"A", "B", "C", "D"}
	counts := make(map[string]int)
	const totalDraws = 6000

	for i := 0; i < totalDraws; i++ {
		res, err := Solve(ids, nil)
		if err != nil {
			t.Fatalf("unexpected error on draw %d: %v", i, err)
		}
		// Canonicalize cycle starting from A
		var sb strings.Builder
		curr := "A"
		for j := 0; j < 4; j++ {
			sb.WriteString(curr)
			curr = res.Assignments[curr]
		}
		counts[sb.String()]++
	}

	if len(counts) != 6 {
		t.Fatalf("expected 6 distinct Hamiltonian cycles for N=4, got %d: %v", len(counts), counts)
	}
	for cyc, c := range counts {
		if c < 850 || c > 1150 {
			t.Errorf("cycle %s count %d outside uniform tolerance [850, 1150] (expected ~1000)", cyc, c)
		}
	}
}

func TestSolveSubLoopsFallback6People(t *testing.T) {
	// Two groups of 3 (G1: P1,P2,P3; G2: P4,P5,P6) completely excluded from each other.
	// Single 6-cycle is impossible, but two 3-cycles exist.
	ids := makeParticipants(6)
	var exclusions []ExclusionPair
	for i := 0; i < 3; i++ {
		for j := 3; j < 6; j++ {
			exclusions = append(exclusions, ExclusionPair{A: ids[i], B: ids[j]})
		}
	}

	res, err := Solve(ids, exclusions)
	if err != nil {
		t.Fatalf("expected sub-loops fallback to succeed, got error: %v", err)
	}
	if res.Mode != ModeSubLoops {
		t.Fatalf("expected ModeSubLoops, got %s", res.Mode)
	}
	numCycles, minLen, maxLen := countCycles(res.Assignments)
	if numCycles != 2 || minLen != 3 || maxLen != 3 {
		t.Fatalf("expected 2 cycles of length 3, got %d cycles (min=%d, max=%d)", numCycles, minLen, maxLen)
	}
	// Verify exclusions and no 2-cycles
	for _, ex := range exclusions {
		if res.Assignments[ex.A] == ex.B || res.Assignments[ex.B] == ex.A {
			t.Fatalf("violated exclusion %s <-> %s", ex.A, ex.B)
		}
	}
}

func TestSolveImpossibleCases(t *testing.T) {
	// 1. N=3 with 1 mutual exclusion -> impossible
	ids3 := makeParticipants(3)
	_, err := Solve(ids3, []ExclusionPair{{A: "P1", B: "P2"}})
	if !errors.Is(err, ErrNoValidDraw) {
		t.Fatalf("N=3 with 1 exclusion: expected ErrNoValidDraw, got %v", err)
	}

	// 2. N=4 where only two 2-cycles exist (P1-P2 disconnected from P3-P4) -> rejected
	ids4 := makeParticipants(4)
	ex4 := []ExclusionPair{
		{A: "P1", B: "P3"}, {A: "P1", B: "P4"},
		{A: "P2", B: "P3"}, {A: "P2", B: "P4"},
	}
	_, err = Solve(ids4, ex4)
	if !errors.Is(err, ErrNoValidDraw) {
		t.Fatalf("N=4 disconnected pairs: expected ErrNoValidDraw, got %v", err)
	}

	// 3. N=5 where a 5-cycle is impossible (bipartite K_{2,3} where the 2-side has no internal edge and 3-side has no internal edges)
	ids5 := makeParticipants(5)
	// Exclude P3, P4, P5 from each other -> independent set of size 3 in a 5-vertex graph cannot have a Hamiltonian cycle, and 5 cannot be partitioned into >=3 sub-loops
	ex5 := []ExclusionPair{
		{A: "P3", B: "P4"},
		{A: "P4", B: "P5"},
		{A: "P3", B: "P5"},
	}
	_, err = Solve(ids5, ex5)
	if !errors.Is(err, ErrNoValidDraw) {
		t.Fatalf("N=5 impossible 5-cycle: expected ErrNoValidDraw, got %v", err)
	}
}

func TestSolveAntiDoS50Participants(t *testing.T) {
	// N=50 bipartite K_{20,30} where all vertices have degree >= 20 (passes quick degree check),
	// but no odd cycle or balanced cycle can exist because independent set of size 30 > 25!
	ids := makeParticipants(50)
	var exclusions []ExclusionPair
	// Make P21..P50 an independent set of size 30 (all mutually excluded from each other)
	for i := 20; i < 50; i++ {
		for j := i + 1; j < 50; j++ {
			exclusions = append(exclusions, ExclusionPair{A: ids[i], B: ids[j]})
		}
	}

	start := time.Now()
	_, err := Solve(ids, exclusions)
	elapsed := time.Since(start)
	if !errors.Is(err, ErrNoValidDraw) {
		t.Fatalf("expected ErrNoValidDraw for unsatisfiable N=50 graph, got %v", err)
	}
	if elapsed > 600*time.Millisecond {
		t.Fatalf("anti-DoS check failed: took %v (limit 600ms)", elapsed)
	}
}
