package ivfsearch

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
)

const (
	Dim          = 14
	IvfClusters  = 4096
	IvfMaxNprobe = 64
	KNeighbors   = 5
	FixScale     = 10000.0
	VectorScale  = 1.0 / FixScale
	BlockStride  = 112 // 8 slots * 14 dims
)

var (
	gDataset dataset
	gNprobe     = 8
	gFullNprobe = 24
	gCandidates = 0
)

type dataset struct {
	n            int
	totalBlocks  int
	centroidsT   []float32 // [DIM * IvfClusters], transposed dim-major
	blockOffsets [IvfClusters + 1]uint32
	labels       []uint8 // [totalBlocks * 8], padded
	blocks       []int16 // [totalBlocks * BlockStride], AoSoA
}

func LoadIndex(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var magic [4]byte
	var n, k, d uint32
	if err := binary.Read(f, binary.LittleEndian, &magic); err != nil {
		return fmt.Errorf("read magic: %w", err)
	}
	if magic != [4]byte{'I', 'V', 'F', '1'} {
		return fmt.Errorf("invalid magic: %s", string(magic[:]))
	}
	if err := binary.Read(f, binary.LittleEndian, &n); err != nil {
		return err
	}
	if err := binary.Read(f, binary.LittleEndian, &k); err != nil {
		return err
	}
	if err := binary.Read(f, binary.LittleEndian, &d); err != nil {
		return err
	}

	if k != IvfClusters || d != Dim {
		return fmt.Errorf("incompatible index: K=%d D=%d", k, d)
	}

	gDataset.n = int(n)

	// Transposed centroids
	centroidsSize := Dim * IvfClusters
	gDataset.centroidsT = make([]float32, centroidsSize)
	if err := binary.Read(f, binary.LittleEndian, gDataset.centroidsT); err != nil {
		return fmt.Errorf("read centroids: %w", err)
	}

	// Block offsets
	if err := binary.Read(f, binary.LittleEndian, gDataset.blockOffsets[:]); err != nil {
		return fmt.Errorf("read block_offsets: %w", err)
	}
	gDataset.totalBlocks = int(gDataset.blockOffsets[IvfClusters])
	paddedN := gDataset.totalBlocks * 8

	// Labels
	gDataset.labels = make([]uint8, paddedN)
	if err := binary.Read(f, binary.LittleEndian, gDataset.labels); err != nil {
		return fmt.Errorf("read labels: %w", err)
	}

	// Blocks
	blockCount := gDataset.totalBlocks * BlockStride
	gDataset.blocks = make([]int16, blockCount)
	if err := binary.Read(f, binary.LittleEndian, gDataset.blocks); err != nil {
		return fmt.Errorf("read blocks: %w", err)
	}

	mb := float64(blockCount*2+paddedN+centroidsSize*4+(IvfClusters+1)*4) / (1024.0 * 1024.0)
	fmt.Fprintf(os.Stderr, "index IVF1 loaded (pure Go): N=%d K=%d blocks=%d memory=%.2f MB\n",
		n, k, gDataset.totalBlocks, mb)

	return nil
}

func SetParams(nprobe, fullNprobe, candidates int) {
	gNprobe = nprobe
	gFullNprobe = fullNprobe
	gCandidates = candidates
}

// quantizeFixed converts float to int16 fixed point
func quantizeFixed(x float32) int16 {
	if x < -1.0 {
		x = -1.0
	}
	if x > 1.0 {
		x = 1.0
	}
	scaled := x * FixScale
	if scaled >= 0 {
		scaled += 0.5
	} else {
		scaled -= 0.5
	}
	if scaled < -10000.0 {
		scaled = -10000.0
	}
	if scaled > 10000.0 {
		scaled = 10000.0
	}
	return int16(scaled)
}

// tryInsertTop5 inserts into sorted top-5 list
//
//go:nosplit
func tryInsertTop5(d float32, label uint8, bestD *[5]float32, bestL *[5]uint8, worstD *float32) {
	if d >= *worstD {
		return
	}
	pos := 4
	for pos > 0 && d < bestD[pos-1] {
		pos--
	}
	for i := 4; i > pos; i-- {
		bestD[i] = bestD[i-1]
		bestL[i] = bestL[i-1]
	}
	bestD[pos] = d
	bestL[pos] = label
	*worstD = bestD[4]
}

// computeCentroidDists computes squared distance from query to all centroids
// centroidsT is laid out as [dim0_cluster0, dim0_cluster1, ..., dim0_cluster4095, dim1_cluster0, ...]
//
//go:nosplit
func computeCentroidDists(qGrid *[Dim]float32, dists []float32) {
	ct := gDataset.centroidsT
	k := IvfClusters

	// Dim 0: initialize
	q0 := qGrid[0]
	for c := 0; c < k; c += 8 {
		diff0 := ct[c+0] - q0
		diff1 := ct[c+1] - q0
		diff2 := ct[c+2] - q0
		diff3 := ct[c+3] - q0
		diff4 := ct[c+4] - q0
		diff5 := ct[c+5] - q0
		diff6 := ct[c+6] - q0
		diff7 := ct[c+7] - q0
		dists[c+0] = diff0 * diff0
		dists[c+1] = diff1 * diff1
		dists[c+2] = diff2 * diff2
		dists[c+3] = diff3 * diff3
		dists[c+4] = diff4 * diff4
		dists[c+5] = diff5 * diff5
		dists[c+6] = diff6 * diff6
		dists[c+7] = diff7 * diff7
	}

	// Dims 1..13: accumulate
	for d := 1; d < Dim; d++ {
		base := d * k
		qd := qGrid[d]
		for c := 0; c < k; c += 8 {
			diff0 := ct[base+c+0] - qd
			diff1 := ct[base+c+1] - qd
			diff2 := ct[base+c+2] - qd
			diff3 := ct[base+c+3] - qd
			diff4 := ct[base+c+4] - qd
			diff5 := ct[base+c+5] - qd
			diff6 := ct[base+c+6] - qd
			diff7 := ct[base+c+7] - qd
			dists[c+0] += diff0 * diff0
			dists[c+1] += diff1 * diff1
			dists[c+2] += diff2 * diff2
			dists[c+3] += diff3 * diff3
			dists[c+4] += diff4 * diff4
			dists[c+5] += diff5 * diff5
			dists[c+6] += diff6 * diff6
			dists[c+7] += diff7 * diff7
		}
	}
}

// topNFromDists finds top-N smallest distances
//
//go:nosplit
func topNFromDists(nprobe int, dists []float32, bestC *[IvfMaxNprobe]int, bestP *[IvfMaxNprobe]float32) {
	for i := 0; i < nprobe; i++ {
		bestC[i] = -1
		bestP[i] = float32(math.Inf(1))
	}

	worstP := bestP[nprobe-1]

	for c := 0; c < IvfClusters; c++ {
		di := dists[c]
		if di >= worstP {
			continue
		}
		// Insert into sorted top-N
		pos := nprobe - 1
		for pos > 0 && di < bestP[pos-1] {
			pos--
		}
		for i := nprobe - 1; i > pos; i-- {
			bestP[i] = bestP[i-1]
			bestC[i] = bestC[i-1]
		}
		bestP[pos] = di
		bestC[pos] = c
		worstP = bestP[nprobe-1]
	}
}

// scanBlocks scans AoSoA blocks for nearest neighbors
//
//go:nosplit
func scanBlocks(startBlock, endBlock int, q *[Dim]int16, bestD *[5]float32, bestL *[5]uint8, worstD *float32) {
	blocks := gDataset.blocks
	labels := gDataset.labels
	vscale := float32(VectorScale)

	// Pre-compute query scaled values
	var qScaled [Dim]float32
	for j := 0; j < Dim; j++ {
		qScaled[j] = float32(q[j]) * vscale
	}

	for bi := startBlock; bi < endBlock; bi++ {
		bb := bi * BlockStride
		lb := labels[bi*8:]

		// Process 8 slots per block
		for slot := 0; slot < 8; slot++ {
			if blocks[bb+slot] == int16(math.MaxInt16) {
				continue
			}

			var dist float32
			for j := 0; j < Dim; j++ {
				dv := float32(blocks[bb+j*8+slot]) * vscale
				diff := dv - qScaled[j]
				dist += diff * diff
			}

			if dist < *worstD {
				tryInsertTop5(dist, lb[slot], bestD, bestL, worstD)
			}
		}
	}
}

//go:nosplit
func searchWithNprobe(nprobe int, bestC *[IvfMaxNprobe]int, q *[Dim]int16) int {
	if nprobe > IvfClusters {
		nprobe = IvfClusters
	}
	if nprobe < 1 {
		nprobe = 1
	}

	bestD := [5]float32{float32(math.Inf(1)), float32(math.Inf(1)), float32(math.Inf(1)), float32(math.Inf(1)), float32(math.Inf(1))}
	bestL := [5]uint8{}
	worstD := float32(math.Inf(1))

	for pi := 0; pi < nprobe; pi++ {
		c := bestC[pi]
		if c < 0 {
			continue
		}
		startBlock := int(gDataset.blockOffsets[c])
		endBlock := int(gDataset.blockOffsets[c+1])
		if endBlock <= startBlock {
			continue
		}

		if gCandidates > 0 {
			maxBlocks := (gCandidates + 7) / 8
			nblocks := endBlock - startBlock
			if nblocks > maxBlocks {
				endBlock = startBlock + maxBlocks
			}
		}

		scanBlocks(startBlock, endBlock, q, &bestD, &bestL, &worstD)
	}

	// Count frauds
	count := 0
	for i := 0; i < 5; i++ {
		if bestL[i] == 1 {
			count++
		}
	}
	return count
}

// Search performs IVF search on the query vector, returns fraud count (0-5)
func Search(qFloat [Dim]float32) int {
	// Quantize
	var q [Dim]int16
	var qGrid [Dim]float32
	for j := 0; j < Dim; j++ {
		q[j] = quantizeFixed(qFloat[j])
		qGrid[j] = float32(q[j]) / FixScale
	}

	fastNprobe := gNprobe
	if fastNprobe < 1 {
		fastNprobe = 1
	}
	if fastNprobe > IvfMaxNprobe {
		fastNprobe = IvfMaxNprobe
	}
	if fastNprobe > IvfClusters {
		fastNprobe = IvfClusters
	}

	fullNprobe := gFullNprobe
	if fullNprobe < fastNprobe {
		fullNprobe = fastNprobe
	}
	if fullNprobe > IvfMaxNprobe {
		fullNprobe = IvfMaxNprobe
	}
	if fullNprobe > IvfClusters {
		fullNprobe = IvfClusters
	}

	// Compute centroid distances
	var dists [IvfClusters]float32
	computeCentroidDists(&qGrid, dists[:])

	// Top-N clusters
	var bestC [IvfMaxNprobe]int
	var bestP [IvfMaxNprobe]float32
	topNFromDists(fullNprobe, dists[:], &bestC, &bestP)

	// Fast pass
	result := searchWithNprobe(fastNprobe, &bestC, &q)

	// Two-stage: if ambiguous, re-run with full probes
	if result == 2 || result == 3 {
		result = searchWithNprobe(fullNprobe, &bestC, &q)
	}

	return result
}
