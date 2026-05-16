package ivfsearch

//go:noescape
func computeCentroidDistsAVX2(qGrid *[14]float32, ct *float32, dists *float32)

//go:noescape
func computeBlocksAVX2(blocks *int16, nblocks int, qScaled *[16]float32, dists *float32)
