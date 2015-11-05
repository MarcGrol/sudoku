package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const square_size = 9
const section_size = 3

type Game struct {
	square     *Square
	StepCount  int
	GuessCount int
}

func newGame() *Game {
	g := Game{}
	g.square = NewSquare(square_size)
	return &g
}

func (g Game) copy() *Game {
	ng := Game{}
	ng.square = g.square.Copy()
	ng.StepCount = g.StepCount
	ng.GuessCount = g.GuessCount
	return &ng
}

func Load(lines string) (*Game, error) {
	linesRead := 0
	game := newGame()
	for x, line := range strings.Split(lines, "\n") {

		if x >= square_size {
			break
		}
		if line == "" {
			break
		}

		splitted := strings.Split(line, " ")
		if len(splitted) != square_size {
			return nil, fmt.Errorf("Invalid number of columns for row %d: needs %d, actual %d", x+1, square_size, len(splitted))
		}
		for y, val := range splitted {
			if val == "_" {
				game.square.Clear(x, y)
				continue
			}
			num, err := strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("Invalid number '%s' for item row:%d, column:%d", val, x+1, y+1)
			}
			if num < 0 || num > square_size {
				return nil, fmt.Errorf("Invalid value %d for item row:%d, column:%d", num, x+1, y+1)

			}
			game.square.Set(x, y, num)
		}
		linesRead++
	}
	if linesRead != square_size {
		return nil, fmt.Errorf("Not enough rows: needs %d, actual %d", square_size, linesRead)
	}

	err := game.validate()
	if err != nil {
		return nil, err
	}
	return game, nil
}

func (g Game) validate() error {
	// check for duplicate values in each row
	for x := 0; x < g.square.Size; x++ {
		row := g.square.GetRowValues(x)
		hasDups, dupVal := containsDuplicates(row)
		if hasDups {
			return fmt.Errorf("Row %d contains duplicate value %d", x+1, dupVal)
		}
	}

	// check for duplicate values in each column
	for y := 0; y < g.square.Size; y++ {
		column := g.square.GetColumnValues(y)
		hasDups, dupVal := containsDuplicates(column)
		if hasDups {
			return fmt.Errorf("Column %d contains duplicate value %d", y+1, dupVal)
		}
	}

	// check for duplicate values in each section
	// only visit the centers of each section
	for x := 1; x < g.square.Size; x += section_size {
		for y := 1; y < g.square.Size; y += section_size {
			section := g.square.GetSectionValues(x, y)
			hasDups, dupVal := containsDuplicates(section)
			if hasDups {
				return fmt.Errorf("Cell %d-%d is in section with duplicate value %d", x+1, y+1, dupVal)
			}
		}
	}

	return nil
}

func containsDuplicates(array []int) (bool, int) {
	for idx, i := range array {
		if contains(array, i, idx) {
			return true, i
		}
	}
	return false, -1
}

func contains(array []int, value int, selfIdx int) bool {
	for idx, i := range array {
		if idx == selfIdx {
			continue
		}
		if value == i {
			return true
		}
	}
	return false
}

func Solve(g *Game, timeout int) ([]*Game, error) {
	resultChannel := make(chan *Game)

	go solve(g, resultChannel)

	timer := time.After(time.Duration(timeout) * time.Second)

	solutions := make([]*Game, 0, 10)
Select_loop:
	for {
		select {
		case solution := <-resultChannel:
			solutions = append(solutions, solution)
			if len(solutions) >= 1 {
				break Select_loop
			}
		case <-timer:
			fmt.Fprintf(os.Stderr, "Timeout %d expired\n", timeout)
			break Select_loop
		}
	}
	return solutions, nil
}

func solve(g *Game, resultChannel chan *Game) {
	maxSteps := square_size * square_size

	for i := 0; i < maxSteps; i++ {

		cellsSolvedInStep := g.step()

		if cellsSolvedInStep < 0 {
			// wrong guess upstream, terminate go-routine
			return
		}

		if cellsSolvedInStep == 0 {
			// stuck using deterministic approach: start guessing
			guessAndContinue(g, resultChannel)
			return
		}

		fmt.Fprintf(os.Stderr, "%p: Solved %d cells in step %d\n", g, cellsSolvedInStep, g.StepCount)

		if g.countEmptyValues() == 0 && g.validate() == nil {
			// we are done: report result back over solution-channel
			resultChannel <- g
			return
		}
	}

	// unsolveable
	fmt.Fprintf(os.Stderr, "%p: Abort after steps:%d\n", g, g.StepCount)
}

func (g *Game) step() int {
	defer func() {
		g.StepCount++
	}()

	cellsSolved := 0

	for x := 0; x < g.square.Size; x++ {
		for y := 0; y < g.square.Size; y++ {
			if !g.square.Has(x, y) {
				mergedCandidates := g.findCandidates(x, y)
				if len(mergedCandidates) == 0 {
					// we have mad a wrong guess somwhere
					fmt.Fprintf(os.Stderr, "%p: Cell %d-%d has zero candidates due to wrong guess\n", g, x+1, y+1)
					return -1
				} else if len(mergedCandidates) == 1 {
					g.square.Set(x, y, mergedCandidates[0])
					cellsSolved++
				}
			}
		}
	}

	return cellsSolved
}

func guessAndContinue(g *Game, resultChannel chan *Game) {
	orderedBestGuesses := g.findCellsWithLeastCandidates()

	for _, guess := range orderedBestGuesses {
		for _, cand := range guess.candidates {
			fmt.Fprintf(os.Stderr, "%p: Try %d-%d to %d and continue\n", g, guess.x+1, guess.y+1, cand)
			cpy := g.copy()
			cpy.square.Set(guess.x, guess.y, cand)
			g.GuessCount++
			go solve(cpy, resultChannel)
		}
	}
}

func (g *Game) findCellsWithLeastCandidates() []cell {
	cells := make([]cell, 0, square_size*square_size)
	g.square.Iterate(func(x int, y int, z int) error {
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

func (g *Game) findCandidates(x int, y int) []int {
	mergedValues := mergeValues(
		g.square.GetRowValues(x),
		g.square.GetColumnValues(y),
		g.square.GetSectionValues(x, y))
	sort.Ints(mergedValues)
	return findCandidates(mergedValues)
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

func (g *Game) countEmptyValues() int {
	count := 0
	g.square.Iterate(func(x, y, z int) error {
		if !g.square.Has(x, y) {
			count++
		}
		return nil
	})
	return count
}

func mergeValues(rowValues []int, columnValues []int, sectionValues []int) []int {
	merged := append(rowValues, columnValues...)
	return append(merged, sectionValues...)
}

func findCandidates(existing []int) []int {
	full := makeFull(square_size)
	candidates := minus(full, existing)

	return candidates
}

func makeFull(size int) []int {
	full := make([]int, size)
	for i := 0; i < size; i++ {
		full[i] = (i + 1)
	}
	return full
}

func minus(all []int, other []int) []int {
	stripped := make([]int, 0, len(all))
	for _, a := range all {
		if !contains(other, a, -1) {
			stripped = append(stripped, a)
		}
	}
	return stripped
}

type cell struct {
	x          int
	y          int
	candidates []int
}
type CellByNumberOfCandidates []cell

func (arr CellByNumberOfCandidates) Len() int      { return len(arr) }
func (arr CellByNumberOfCandidates) Swap(i, j int) { arr[i], arr[j] = arr[j], arr[i] }
func (arr CellByNumberOfCandidates) Less(i, j int) bool {
	return len(arr[i].candidates) < len(arr[j].candidates)
}

func (g Game) String() string {
	return g.square.String()
}
