package main

type nodeStore map[int]*simNode

func makeStoreSingle() nodeStore {
	s := make(nodeStore)
	s[0] = newNode(0)
	return s
}

func linkNodes(a *simNode, b *simNode) {
	la := a.core.NewSimlink()
	lb := b.core.NewSimlink()
	la.SetDestination(lb)
	lb.SetDestination(la)
	la.Start()
	lb.Start()
}

func makeStoreSquareGrid(sideLength int) nodeStore {
	store := make(nodeStore)
	nNodes := sideLength * sideLength
	idxs := make([]int, 0, nNodes)
	// TODO shuffle nodeIDs
	for idx := 1; idx <= nNodes; idx++ {
		idxs = append(idxs, idx)
	}
	for _, idx := range idxs {
		n := newNode(idx)
		store[idx] = n
	}
	for idx := 0; idx < nNodes; idx++ {
		if (idx % sideLength) != 0 {
			linkNodes(store[idxs[idx]], store[idxs[idx-1]])
		}
		if idx >= sideLength {
			linkNodes(store[idxs[idx]], store[idxs[idx-sideLength]])
		}
	}
	return store
}
