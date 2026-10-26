package draw

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

var (
	ErrTooFewParticipants = errors.New("at least 3 participants are required")
	ErrTooManyParticipants = errors.New("at most 100 participants are supported")
	ErrNoValidDraw        = errors.New("no valid draw possible with current exclusions")
)

type Mode string

const (
	ModeSingleCycle Mode = "single_cycle"
	ModeSubLoops    Mode = "sub_loops"
	ModeImpossible  Mode = "impossible"
)

type ExclusionPair struct {
	A string
	B string
}

type Result struct {
	Assignments map[string]string
	Mode        Mode
	Diagnostic  string
}

func Solve(participantIDs []string, exclusions []ExclusionPair) (*Result, error) {
	n := len(participantIDs)
	if n < 3 {
		return &Result{Mode: ModeImpossible, Diagnostic: "At least 3 participants are required"}, ErrTooFewParticipants
	}
	if n > 100 {
		return &Result{Mode: ModeImpossible, Diagnostic: "Maximum 100 participants"}, ErrTooManyParticipants
	}

	idxByID := make(map[string]int, n)
	for i, id := range participantIDs {
		idxByID[id] = i
	}

	// Build adjacency matrix of allowed directed edges (symmetric since exclusions are mutual)
	allowed := make([][]bool, n)
	for i := range allowed {
		allowed[i] = make([]bool, n)
		for j := range allowed[i] {
			if i != j {
				allowed[i][j] = true
			}
		}
	}
	for _, ex := range exclusions {
		i, okA := idxByID[ex.A]
		j, okB := idxByID[ex.B]
		if okA && okB {
			allowed[i][j] = false
			allowed[j][i] = false
		}
	}

	// Quick degree check: in any valid draw (single cycle or >=3 sub-loops),
	// every participant is in a cycle with two distinct neighbors (giver != receiver),
	// so every participant needs at least 2 allowed partners in a mutual exclusion graph!
	for i := 0; i < n; i++ {
		deg := 0
		for j := 0; j < n; j++ {
			if allowed[i][j] {
				deg++
			}
		}
		if deg < 2 {
			diag := fmt.Sprintf("Participant %s has fewer than 2 allowed partners", participantIDs[i])
			return &Result{Mode: ModeImpossible, Diagnostic: diag}, fmt.Errorf("%w: %s", ErrNoValidDraw, diag)
		}
	}

	// Pass 1A: Equiprobable rejection sampling of a single Hamiltonian cycle (K = 2000 attempts).
	// Fix vertex 0 as the start of the cycle and uniformly shuffle vertices 1..n-1.
	perm := make([]int, n-1)
	for i := range perm {
		perm[i] = i + 1
	}

	const maxRejectionAttempts = 2000
	for attempt := 0; attempt < maxRejectionAttempts; attempt++ {
		cryptoShuffle(perm)
		if isValidCycle(perm, allowed) {
			assignments := make(map[string]string, n)
			assignments[participantIDs[0]] = participantIDs[perm[0]]
			for i := 0; i < len(perm)-1; i++ {
				assignments[participantIDs[perm[i]]] = participantIDs[perm[i+1]]
			}
			assignments[participantIDs[perm[len(perm)-1]]] = participantIDs[0]
			return &Result{
				Assignments: assignments,
				Mode:        ModeSingleCycle,
			}, nil
		}
	}

	// Pass 1B: Bounded MRV backtracking for a single Hamiltonian cycle (250ms / 100k steps).
	if cycle, ok := solveHamiltonianMRV(n, allowed, 250*time.Millisecond, 100000); ok {
		assignments := make(map[string]string, n)
		for u, v := range cycle {
			assignments[participantIDs[u]] = participantIDs[v]
		}
		return &Result{
			Assignments: assignments,
			Mode:        ModeSingleCycle,
		}, nil
	}

	// Pass 2: Fallback to sub-loops of size >= 3 (no self-assignment, no 2-cycles).
	if n >= 6 {
		if permMap, ok := solveSubLoopsMRV(n, allowed, 250*time.Millisecond, 100000); ok {
			assignments := make(map[string]string, n)
			for u, v := range permMap {
				assignments[participantIDs[u]] = participantIDs[v]
			}
			return &Result{
				Assignments: assignments,
				Mode:        ModeSubLoops,
			}, nil
		}
	}

	diag := "Exclusions prevent forming a valid draw without reciprocal pairs"
	return &Result{Mode: ModeImpossible, Diagnostic: diag}, fmt.Errorf("%w: %s", ErrNoValidDraw, diag)
}

func isValidCycle(perm []int, allowed [][]bool) bool {
	if !allowed[0][perm[0]] {
		return false
	}
	for i := 0; i < len(perm)-1; i++ {
		if !allowed[perm[i]][perm[i+1]] {
			return false
		}
	}
	return allowed[perm[len(perm)-1]][0]
}

func solveHamiltonianMRV(n int, allowed [][]bool, timeout time.Duration, maxSteps int) ([]int, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	visited := make([]bool, n)
	nextVertex := make([]int, n)
	for i := range nextVertex {
		nextVertex[i] = -1
	}
	visited[0] = true

	// Precompute candidate lists
	adj := make([][]int, n)
	remDeg := make([]int, n)
	for u := 0; u < n; u++ {
		for v := 0; v < n; v++ {
			if allowed[u][v] {
				adj[u] = append(adj[u], v)
				remDeg[u]++
			}
		}
	}

	steps := 0
	var dfs func(curr, depth int) bool
	dfs = func(curr, depth int) bool {
		steps++
		if steps > maxSteps {
			return false
		}
		if steps&63 == 0 && ctx.Err() != nil {
			return false
		}

		if depth == n {
			if allowed[curr][0] {
				nextVertex[curr] = 0
				return true
			}
			return false
		}

		// Gather unvisited neighbors of curr
		var cands []int
		for _, v := range adj[curr] {
			if !visited[v] {
				cands = append(cands, v)
			}
		}
		if len(cands) == 0 {
			return false
		}

		// Shuffle first for randomness among ties, then sort by MRV (fewest remaining unvisited neighbors)
		cryptoShuffle(cands)
		for i := 1; i < len(cands); i++ {
			key := cands[i]
			keyDeg := remDeg[key]
			j := i - 1
			for j >= 0 && remDeg[cands[j]] > keyDeg {
				cands[j+1] = cands[j]
				j--
			}
			cands[j+1] = key
		}

		for _, v := range cands {
			if steps > maxSteps || (steps&63 == 0 && ctx.Err() != nil) {
				return false
			}
			visited[v] = true
			nextVertex[curr] = v
			possible := true
			for _, w := range adj[v] {
				if !visited[w] {
					remDeg[w]--
					if remDeg[w] == 0 && depth+1 < n && !allowed[w][0] {
						possible = false
					}
				}
			}

			if possible && dfs(v, depth+1) {
				return true
			}

			for _, w := range adj[v] {
				if !visited[w] {
					remDeg[w]++
				}
			}
			visited[v] = false
			nextVertex[curr] = -1
		}
		return false
	}

	if dfs(0, 1) {
		return nextVertex, true
	}
	return nil, false
}

func solveSubLoopsMRV(n int, allowed [][]bool, timeout time.Duration, maxSteps int) ([]int, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	assignedOut := make([]int, n)
	assignedIn := make([]int, n)
	for i := 0; i < n; i++ {
		assignedOut[i] = -1
		assignedIn[i] = -1
	}

	adj := make([][]int, n)
	for u := 0; u < n; u++ {
		for v := 0; v < n; v++ {
			if allowed[u][v] {
				adj[u] = append(adj[u], v)
			}
		}
	}

	steps := 0

	// Check if assigning u -> v closes a cycle of length < 3 or leaves remainder impossible
	createsShortCycle := func(u, v int) (bool, int) {
		length := 1
		curr := v
		for curr != -1 && curr != u {
			curr = assignedOut[curr]
			length++
		}
		if curr == u {
			return length < 3, length
		}
		return false, 0
	}

	var backtrack func(assignedCount int) bool
	backtrack = func(assignedCount int) bool {
		steps++
		if steps > maxSteps {
			return false
		}
		if steps&63 == 0 && ctx.Err() != nil {
			return false
		}

		if assignedCount == n {
			return true
		}

		// Pick unassigned vertex u with minimum valid outgoing targets (MRV)
		bestU := -1
		var bestCands []int
		for u := 0; u < n; u++ {
			if assignedOut[u] != -1 {
				continue
			}
			var cands []int
			for _, v := range adj[u] {
				if assignedIn[v] != -1 {
					continue
				}
				if assignedOut[v] == u {
					continue // direct 2-cycle check
				}
				short, _ := createsShortCycle(u, v)
				if short {
					continue
				}
				cands = append(cands, v)
			}
			if len(cands) == 0 {
				return false
			}
			if bestU == -1 || len(cands) < len(bestCands) {
				bestU = u
				bestCands = cands
			}
		}

		// Also verify every unassigned incoming vertex still has at least one valid giver
		for v := 0; v < n; v++ {
			if assignedIn[v] != -1 {
				continue
			}
			hasGiver := false
			for _, u := range adj[v] {
				if assignedOut[u] == -1 && assignedOut[v] != u {
					hasGiver = true
					break
				}
			}
			if !hasGiver {
				return false
			}
		}

		cryptoShuffle(bestCands)
		for _, v := range bestCands {
			if steps > maxSteps || (steps&63 == 0 && ctx.Err() != nil) {
				return false
			}
			// Also check that if closing a cycle leaves 1 or 2 vertices total in the whole graph, we prune early
			_, cycLen := createsShortCycle(uOrBest(bestU), v)
			remainingUnassigned := n - (assignedCount + 1)
			if cycLen >= 3 && (remainingUnassigned == 1 || remainingUnassigned == 2) {
				// Wait: remainingUnassigned is the number of unassigned edges, not necessarily isolated vertices,
				// so only prune if all remaining edges are completely closed cycles.
				allClosed := true
				assignedOut[bestU] = v
				assignedIn[v] = bestU
				for k := 0; k < n; k++ {
					if (assignedOut[k] == -1) != (assignedIn[k] == -1) {
						allClosed = false
						break
					}
				}
				if allClosed && (remainingUnassigned == 1 || remainingUnassigned == 2) {
					assignedOut[bestU] = -1
					assignedIn[v] = -1
					continue
				}
				if backtrack(assignedCount + 1) {
					return true
				}
				assignedOut[bestU] = -1
				assignedIn[v] = -1
				continue
			}

			assignedOut[bestU] = v
			assignedIn[v] = bestU
			if backtrack(assignedCount + 1) {
				return true
			}
			assignedOut[bestU] = -1
			assignedIn[v] = -1
		}
		return false
	}

	if backtrack(0) {
		return assignedOut, true
	}
	return nil, false
}

func uOrBest(u int) int { return u }

func cryptoShuffle(slice []int) {
	n := len(slice)
	if n <= 1 {
		return
	}
	buf := make([]byte, 8*n)
	_, _ = rand.Read(buf)
	for i := n - 1; i > 0; i-- {
		// #nosec G115 -- modulo (i+1) bounds value to [0, i], guaranteed to fit in int
		j := int(binary.LittleEndian.Uint64(buf[i*8:(i+1)*8]) % uint64(i+1))
		slice[i], slice[j] = slice[j], slice[i]
	}
}
