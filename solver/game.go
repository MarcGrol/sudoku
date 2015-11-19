package solver

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

const SQUARE_SIZE = 9
const SECTION_SIZE = 3

var (
	Verbose bool = false
)

type Game struct {
	CellsToBeSolved int
	GuessCount      int
	Steps           []Step
	square          *Square
	solutionChannel chan *Game
	deadline        time.Time
}

type Step struct {
	X       int
	Y       int
	Z       Value
	Initial bool
	IsGuess bool
}

func newGame() *Game {
	g := Game{}
	g.square = NewSquare(SQUARE_SIZE)
	return &g
}

func (g Game) copy() *Game {
	ng := Game{}
	ng.square = g.square.Copy()
	ng.CellsToBeSolved = g.CellsToBeSolved
	ng.GuessCount = g.GuessCount
	ng.solutionChannel = g.solutionChannel
	ng.deadline = g.deadline
	for _, s := range g.Steps {
		ng.Steps = append(ng.Steps, Step{X: s.X, Y: s.Y, Z: s.Z})
	}
	return &ng
}

func LoadSteps(steps []Step) (*Game, error) {
	game := newGame()
	for idx, step := range steps {
		if !game.square.Exists(step.X, step.Y) {
			return nil, fmt.Errorf("Invalid offset: %d-%d for step %d", step.X, step.Y, idx)
		}
		if !game.square.IsAllowed(step.X, step.Y, Value(step.Z)) {
			return nil, fmt.Errorf("Duplicate value %d for item row:%d, column:%d for step %d",
				step.Z, step.X, step.Y, idx)
		}
		game.set(step.X, step.Y, Value(step.Z), true, false)
	}
	game.CellsToBeSolved = game.countEmptyValues()
	return game, nil
}

func Load(lines string) (*Game, error) {
	linesRead := 0
	game := newGame()
	for x, line := range strings.Split(lines, "\n") {

		if x >= SQUARE_SIZE {
			break
		}
		if line == "" {
			break
		}

		splitted := strings.Split(line, " ")
		if len(splitted) != SQUARE_SIZE {
			return nil, fmt.Errorf("Invalid number of columns for row %d: needs %d, actual %d", x+1,
				SQUARE_SIZE, len(splitted))
		}
		for y, val := range splitted {
			if val == "_" {
				game.square.Clear(x, y)
				continue
			}
			num, err := strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("Invalid value '%s' for item row:%d, column:%d", val, x+1, y+1)
			}
			if num < 0 || num > SQUARE_SIZE {
				return nil, fmt.Errorf("Invalid value %d for item row:%d, column:%d", num, x+1, y+1)

			}
			if !game.square.IsAllowed(x, y, Value(num)) {
				return nil, fmt.Errorf("Duplicate value %d for item row:%d, column:%d", num, x+1, y+1)
			}
			game.set(x, y, Value(num), true, false)
		}
		linesRead++
	}
	if linesRead != SQUARE_SIZE {
		return nil, fmt.Errorf("Not enough rows: needs %d, actual %d", SQUARE_SIZE, linesRead)
	}

	game.CellsToBeSolved = game.countEmptyValues()
	return game, nil
}

func Solve(g *Game, timeout int, minSolutionCount int) ([]*Game, error) {
	// non-blocking channel to prevent go-routines to block each other on reporting solution
	solutionChannel := make(chan *Game, 1000)
	duration := time.Duration(timeout) * time.Second

	// Store completion variables within game
	g.solutionChannel = solutionChannel
	g.deadline = time.Now().Add(duration)

	// Start solving in background
	// Solutions will be reported back over solutionChannel
	go solve(g)

	// Wait for a solution
	return waitforCompletion(solutionChannel, duration, minSolutionCount)
}

func waitforCompletion(solutionChannel chan *Game, duration time.Duration, minSolutionCount int) ([]*Game, error) {
	timer := time.After(duration)

	solutions := make([]*Game, 0, 10)
outerLoop:
	for {
		select {
		case newSolution := <-solutionChannel:
			if !solutionExists(solutions, newSolution) {
				if Verbose {
					fmt.Fprintf(os.Stderr, "Solution is new:\n")
				}
				solutions = append(solutions, newSolution)
				if len(solutions) >= minSolutionCount {
					if Verbose {
						fmt.Fprintf(os.Stderr, "Enough solutions received: %d\n", len(solutions))
					}
					break outerLoop
				}
			} else {
				if Verbose {
					fmt.Fprintf(os.Stderr, "Solution exists")
				}
			}
		case <-timer:
			if Verbose {
				fmt.Fprintf(os.Stderr, "Timeout expired after %d secs\n", duration)
			}
			break outerLoop
		}
	}

	if len(solutions) == 0 {
		if Verbose {
			return solutions, fmt.Errorf("No solutions found")
		}
	}
	return solutions, nil
}

func solutionExists(solutions []*Game, newSolution *Game) bool {
	for _, s := range solutions {
		if reflect.DeepEqual(s.square, newSolution.square) {
			return true
		}
	}

	return false
}

func solve(g *Game) {
	maxSteps := SQUARE_SIZE * SQUARE_SIZE

	if Verbose {
		fmt.Fprintf(os.Stderr, "%p: Start solving\n", g)
	}
	for i := 0; i < maxSteps; i++ {

		if time.Now().After(g.deadline) {
			if Verbose {
				fmt.Fprintf(os.Stderr, "%p: Abort because deadline expired\n", g)
			}
			return
		}

		cellsSolvedInStep := g.step()

		if cellsSolvedInStep < 0 {
			// wrong guess upstream, terminate go-routine
			return
		}

		if cellsSolvedInStep == 0 {
			// stuck using deterministic approach: start guessing
			guessAndContinue(g)
			return
		}
		if Verbose {
			fmt.Fprintf(os.Stderr, "%p: Solved %d cells this loop\n", g, cellsSolvedInStep)
		}
		if g.countEmptyValues() == 0 {
			if Verbose {
				fmt.Fprintf(os.Stderr, "%p: Got solution\n", g)
			}
			// we are done: report result back over solution-channel
			g.solutionChannel <- g
			return
		}
	}

	// unsolveable
	if Verbose {
		fmt.Fprintf(os.Stderr, "%p: Abort at cells to go:%d\n", g, g.countEmptyValues())
	}
}

func (g *Game) step() int {
	cellsSolved := 0

	for x := 0; x < g.square.Size; x++ {
		for y := 0; y < g.square.Size; y++ {
			if !g.square.Has(x, y) {
				mergedCandidates := g.findCandidates(x, y)
				if len(mergedCandidates) == 0 {
					// we have mad a wrong guess somwhere
					if Verbose {
						fmt.Fprintf(os.Stderr, "%p: Cell %d-%d has zero candidates due to wrong guess upstream\n", g, x+1, y+1)
					}
					return -1
				} else if len(mergedCandidates) == 1 {
					g.set(x, y, mergedCandidates[0], false, false)
					cellsSolved++
				}
			}
		}
	}

	return cellsSolved
}

func (g *Game) set(x int, y int, z Value, initial bool, isGuess bool) {
	g.square.Set(x, y, z)
	g.Steps = append(g.Steps, Step{X: x, Y: y, Z: z, Initial: initial, IsGuess: isGuess})
	if isGuess == true {
		g.GuessCount++
	}
}

func guessAndContinue(g *Game) {
	orderedBestGuesses := g.findCellsWithLeastCandidates()

	if len(orderedBestGuesses) > 0 {
		bestGuess := orderedBestGuesses[0]
		for _, cand := range bestGuess.candidates {
			cpy := g.copy()
			if Verbose {
				fmt.Fprintf(os.Stderr, "%p: Got stuck -> Try %d-%d with value %d and continue\n", cpy, bestGuess.x+1, bestGuess.y+1, cand)
			}
			cpy.set(bestGuess.x, bestGuess.y, cand, false, true)
			go solve(cpy)
		}
	}
}

func (g *Game) findCellsWithLeastCandidates() []cell {
	cells := make([]cell, 0, SQUARE_SIZE*SQUARE_SIZE)
	g.square.Iterate(func(x int, y int, z Value) error {
		if !g.square.Has(x, y) {
			cellCandidates := g.findCandidates(x, y)
			if len(cellCandidates) > 1 {
				cells = append(cells, cell{x: x, y: y, candidates: cellCandidates})
			}
		}
		return nil
	})
	sort.Sort(CellByNumberOfCandidates(cells))

	return cells
}

func (g *Game) findCandidates(x int, y int) []Value {
	mergedValues := mergeValues(
		g.square.GetRowValues(x),
		g.square.GetColumnValues(y),
		g.square.GetSectionValues(x, y))
	return findCandidates(mergedValues)
}

func (g *Game) countEmptyValues() int {
	count := 0
	g.square.Iterate(func(x int, y int, z Value) error {
		if !g.square.Has(x, y) {
			count++
		}
		return nil
	})
	return count
}

func mergeValues(rowValues ValueSet, columnValues ValueSet, sectionValues ValueSet) ValueSet {
	vs := rowValues.Union(columnValues)
	return vs.Union(sectionValues)
}

func findCandidates(existing ValueSet) []Value {
	full := makeFull(SQUARE_SIZE)
	return full.Difference(existing).ToSlice()
}

func makeFull(size int) ValueSet {
	return NewValueSet(1, 2, 3, 4, 5, 6, 7, 8, 9)
}

type cell struct {
	x          int
	y          int
	candidates []Value
}
type CellByNumberOfCandidates []cell

func (arr CellByNumberOfCandidates) Len() int      { return len(arr) }
func (arr CellByNumberOfCandidates) Swap(i, j int) { arr[i], arr[j] = arr[j], arr[i] }
func (arr CellByNumberOfCandidates) Less(i, j int) bool {
	return len(arr[i].candidates) < len(arr[j].candidates)
}

func (g Game) Dump() string {
	return g.square.String()
}

func (g Game) String() string {

	return g.square.String()
}

func (g *Game) DumpGameState() {

	for x := 0; x < g.square.Size; x++ {
		if (x % 3) == 0 {
			fmt.Fprintf(os.Stderr, "___________________________________________________________________________________________________________________\n")
		}
		for y := 0; y < g.square.Size; y++ {
			if (y % 3) == 0 {
				fmt.Fprintf(os.Stderr, "| ")
			}
			if g.square.Has(x, y) {
				fmt.Fprintf(os.Stderr, "%-12d", g.square.Get(x, y))
			} else {
				mergedCandidates := g.findCandidates(x, y)
				alternatives := fmt.Sprintf("%v", mergedCandidates)
				fmt.Fprintf(os.Stderr, "%-12s", alternatives)

			}
		}
		fmt.Fprintf(os.Stderr, "|\n")
	}
	fmt.Fprintf(os.Stderr, "___________________________________________________________________________________________________________________\n")
	fmt.Fprintf(os.Stderr, "\n\n")
}