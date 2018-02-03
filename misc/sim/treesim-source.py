# Tree routing scheme (named Yggdrasil, after the world tree from Norse mythology)
# Steps:
#   1: Pick any node, here I'm using highest nodeID
#   2: Build spanning tree, each node stores path back to root
#     Optionally with weights for each hop
#     Ties broken by preferring a parent with higher degree
#   3: Distance metric: self->peer + (via tree) peer->dest
#   4: Perform (modified) greedy lookup via this metric for each direction (A->B and B->A)
#   5: Source-route traffic using the better of those two paths

# Note: This makes no attempt to simulate a dynamic network
#   E.g. A node's peers cannot be disconnected

# TODO:
#   Make better use of drop?
#   In particular, we should be ignoring *all* recently dropped *paths* to the root
#     To minimize route flapping
#     Not really an issue in the sim, but probably needed for a real network

import array
import gc
import glob
import gzip
import heapq
import os
import random
import time

#############
# Constants #
#############

# Reminder of where link cost comes in
LINK_COST = 1

# Timeout before dropping something, in simulated seconds
TIMEOUT = 60

###########
# Classes #
###########

class PathInfo:
  def __init__(self, nodeID):
    self.nodeID = nodeID   # e.g. IP
    self.coords = []       # Position in tree
    self.tstamp = 0        # Timestamp from sender, to keep track of old vs new info
    self.degree = 0        # Number of peers the sender has, used to break ties
    # The above should be signed
    self.path   = [nodeID] # Path to node (in path-vector route)
    self.time   = 0        # Time info was updated, to keep track of e.g. timeouts
    self.treeID = nodeID   # Hack, let tree use different ID than IP, used so we can dijkstra once and test many roots
  def clone(self):
    # Return a deep-enough copy of the path
    clone = PathInfo(None)
    clone.nodeID = self.nodeID
    clone.coords = self.coords[:]
    clone.tstamp = self.tstamp
    clone.degree = self.degree
    clone.path = self.path[:]
    clone.time = self.time
    clone.treeID = self.treeID
    return clone
# End class PathInfo

class Node:
  def __init__(self, nodeID):
    self.info  = PathInfo(nodeID) # Self NodeInfo
    self.root  = None             # PathInfo to node at root of tree
    self.drop  = dict()           # PathInfo to nodes from clus that have timed out
    self.peers = dict()           # PathInfo to peers
    self.links = dict()           # Links to peers (to pass messages)
    self.msgs  = []               # Said messages
    self.table = dict()           # Pre-computed lookup table of peer info

  def tick(self):
    # Do periodic maintenance stuff, including push updates
    self.info.time += 1
    if self.info.time > self.info.tstamp + TIMEOUT/4:
      # Update timestamp at least once every 1/4 timeout period
      # This should probably be randomized in a real implementation
      self.info.tstamp = self.info.time
      self.info.degree = len(self.peers)
      self.info.degree = 0# TODO decide if degree should be used
    changed = False # Used to track when the network has converged
    changed |= self.cleanRoot()
    self.cleanDropped()
    # Should probably send messages infrequently if there's nothing new to report
    if self.info.tstamp == self.info.time:
      msg = self.createMessage()
      self.sendMessage(msg)
    return changed

  def cleanRoot(self):
    changed = False
    if self.root and self.info.time - self.root.time > TIMEOUT:
      print "DEBUG: clean root,", self.root.path
      self.drop[self.root.treeID] = self.root
      self.root = None
      changed = True
    if not self.root or self.root.treeID < self.info.treeID:
      # No need to drop someone who'se worse than us
      self.info.coords = [self.info.nodeID]
      self.root = self.info.clone()
      changed = True
    elif self.root.treeID == self.info.treeID:
      self.root = self.info.clone()
    return changed

  def cleanDropped(self):
    # May actually be a treeID... better to iterate over keys explicitly
    nodeIDs = sorted(self.drop.keys())
    for nodeID in nodeIDs:
      node = self.drop[nodeID]
      if self.info.time - node.time > 4*TIMEOUT:
        del self.drop[nodeID]
    return None

  def createMessage(self):
    # Message is just a tuple
    # First element is the sender
    # Second element is the root
    # We will .clone() everything during the send operation
    msg = (self.info, self.root)
    return msg

  def sendMessage(self, msg):
    for link in self.links.values():
      newMsg = (msg[0].clone(), msg[1].clone())
      link.msgs.append(newMsg)
    return None

  def handleMessages(self):
    changed = False
    while self.msgs:
      changed |= self.handleMessage(self.msgs.pop())
    return changed

  def handleMessage(self, msg):
    changed = False
    for node in msg:
      # Update the path and timestamp for the sender and root info
      node.path.append(self.info.nodeID)
      node.time = self.info.time
    # Update the sender's info in our list of peers
    sender = msg[0]
    self.peers[sender.nodeID] = sender
    # Decide if we want to update the root
    root = msg[1]
    updateRoot = False
    isSameParent = False
    isBetterParent = False
    if len(self.root.path) > 1 and len(root.path) > 1:
      parent = self.peers[self.root.path[-2]]
      if parent.nodeID == sender.nodeID: isSameParent = True
      if sender.degree > parent.degree:
        # This would also be where you check path uptime/reliability/whatever
        # All else being equal, we prefer parents with high degree
        # We are trusting peers to report degree correctly in this case
        # So expect some performance reduction if your peers aren't trustworthy
        # (Lies can increase average stretch by a few %)
        isBetterParent = True
    if self.info.nodeID in root.path[:-1]: pass # No loopy routes allowed
    elif root.treeID in self.drop and self.drop[root.treeID].tstamp >= root.tstamp: pass
    elif not self.root: updateRoot = True
    elif self.root.treeID < root.treeID: updateRoot = True
    elif self.root.treeID != root.treeID: pass
    elif self.root.tstamp > root.tstamp: pass
    elif len(root.path) < len(self.root.path): updateRoot = True
    elif isBetterParent and len(root.path) == len(self.root.path): updateRoot = True
    elif isSameParent and self.root.tstamp < root.tstamp: updateRoot = True
    if updateRoot:
      if not self.root or self.root.path != root.path: changed = True
      self.root = root
      self.info.coords = self.root.path
    return changed

  def lookup(self, dest):
    # Note: Can loop in an unconverged network
    # The person looking up the route is responsible for checking for loops
    best = None
    bestDist = 0
    bestDeg = 0
    for node in self.peers.itervalues():
      # dist = distance to node + dist (on tree) from node to dest
      dist = len(node.path)-1 + treeDist(node.coords, dest.coords)
      deg = node.degree
      if not best or dist < bestDist or (best == bestDist and deg > bestDeg):
        best = node
        bestDist = dist
        bestDeg = deg
    if best:
      next = best.path[-2]
      assert next in self.peers
      return next
    else:
      # We failed to look something up
      # TODO some way to signal this which doesn't crash
      assert False

  def initTable(self):
    # Pre-computes a lookup table for destination coords
    # Insert parent first so you prefer them as a next-hop
    self.table.clear()
    parent = self.info.nodeID
    if len(self.info.coords) >= 2: parent = self.info.coords[-2]
    for peer in self.peers.itervalues():
      current = self.table
      for coord in peer.coords:
        if coord not in current: current[coord] = (peer.nodeID, dict())
        old = current[coord]
        next = old[1]
        oldPeer = self.peers[old[0]]
        oldDist = len(oldPeer.coords)
        oldDeg = oldPeer.degree
        newDist = len(peer.coords)
        newDeg = peer.degree
        # Prefer parent
        # Else prefer short distance from root
        # If equal distance, prefer high degree
        if peer.nodeID == parent: current[coord] = (peer.nodeID, next)
        elif newDist < oldDist: current[coord] = (peer.nodeID, next)
        elif newDist == oldDist and newDeg > oldDeg: current[coord] = (peer.nodeID, next)
        current = next
    return None

  def lookup_new(self, dest):
    # Use pre-computed lookup table to look up next hop for dest coords
    assert self.table
    if len(self.info.coords) >= 2: parent = self.info.coords[-2]
    else: parent = None
    current = (parent, self.table)
    c = None
    for coord in dest.coords:
      c = coord
      if coord not in current[1]: break
      current = current[1][coord]
    next = current[0]
    if c in self.peers: next = c
    if next not in self.peers:
      assert next == None
      # You're the root of a different connected component
      # You'd drop the packet in this case
      # To make the path cache not die, need to return a valid next hop...
      # Returning self for that reason
      next = self.info.nodeID
    return next
# End class Node

####################
# Helper Functions #
####################

def getIndexOfLCA(source, dest):
  # Return index of last common ancestor in source/dest coords
  # -1 if no common ancestor (e.g. different roots)
  lcaIdx = -1
  minLen = min(len(source), len(dest))
  for idx in xrange(minLen):
    if source[idx] == dest[idx]: lcaIdx = idx
    else: break
  return lcaIdx

def treePath(source, dest):
  # Return path with source at head and dest at tail
  lastMatch = getIndexOfLCA(source, dest)
  path = dest[-1:lastMatch:-1] + source[lastMatch:]
  assert path[0] == dest[-1]
  assert path[-1] == source[-1]
  return path

def treeDist(source, dest):
  dist = len(source) + len(dest)
  lcaIdx = getIndexOfLCA(source, dest)
  dist -= 2*(lcaIdx+1)
  return dist

def dijkstra(nodestore, startingNodeID):
  # Idea to use heapq and basic implementation taken from stackexchange post
  # http://codereview.stackexchange.com/questions/79025/dijkstras-algorithm-in-python
  nodeIDs = sorted(nodestore.keys())
  nNodes = len(nodeIDs)
  idxs = dict()
  for nodeIdx in xrange(nNodes):
    nodeID = nodeIDs[nodeIdx]
    idxs[nodeID] = nodeIdx
  dists = array.array("H", [0]*nNodes)
  queue = [(0, startingNodeID)]
  while queue:
    dist, nodeID = heapq.heappop(queue)
    idx = idxs[nodeID]
    if not dists[idx]: # Unvisited, otherwise we skip it
      dists[idx] = dist
      for peer in nodestore[nodeID].links:
        if not dists[idxs[peer]]:
          # Peer is also unvisited, so add to queue
          heapq.heappush(queue, (dist+LINK_COST, peer))
  return dists

def dijkstrall(nodestore):
  # Idea to use heapq and basic implementation taken from stackexchange post
  # http://codereview.stackexchange.com/questions/79025/dijkstras-algorithm-in-python
  nodeIDs = sorted(nodestore.keys())
  nNodes = len(nodeIDs)
  idxs = dict()
  for nodeIdx in xrange(nNodes):
    nodeID = nodeIDs[nodeIdx]
    idxs[nodeID] = nodeIdx
  dists = array.array("H", [0]*nNodes*nNodes) # use GetCacheIndex(nNodes, start, end)
  for sourceIdx in xrange(nNodes):
    print "Finding shortest paths for node {} / {} ({})".format(sourceIdx+1, nNodes, nodeIDs[sourceIdx])
    queue = [(0, sourceIdx)]
    while queue:
      dist, nodeIdx = heapq.heappop(queue)
      distIdx = getCacheIndex(nNodes, sourceIdx, nodeIdx)
      if not dists[distIdx]: # Unvisited, otherwise we skip it
        dists[distIdx] = dist
        for peer in nodestore[nodeIDs[nodeIdx]].links:
          pIdx = idxs[peer]
          pdIdx = getCacheIndex(nNodes, sourceIdx, pIdx)
          if not dists[pdIdx]:
            # Peer is also unvisited, so add to queue
            heapq.heappush(queue, (dist+LINK_COST, pIdx))
  return dists

def linkNodes(node1, node2):
  node1.links[node2.info.nodeID] = node2
  node2.links[node1.info.nodeID] = node1

############################
# Store topology functions #
############################

def makeStoreSquareGrid(sideLength, randomize=True):
  # Simple grid in a sideLength*sideLength square
  # Just used to validate that the code runs
  store = dict()
  nodeIDs = list(range(sideLength*sideLength))
  if randomize: random.shuffle(nodeIDs)
  for nodeID in nodeIDs:
    store[nodeID] = Node(nodeID)
  for index in xrange(len(nodeIDs)):
    if (index % sideLength != 0): linkNodes(store[nodeIDs[index]], store[nodeIDs[index-1]])
    if (index >= sideLength): linkNodes(store[nodeIDs[index]], store[nodeIDs[index-sideLength]])
  print "Grid store created, size {}".format(len(store))
  return store

def makeStoreASRelGraph(pathToGraph):
  #Existing network graphs, in caida.org's asrel format (ASx|ASy|z per line, z denotes relationship type)
  with open(pathToGraph, "r") as f:
    inData = f.readlines()
  store = dict()
  for line in inData:
    if line.strip()[0] == "#": continue # Skip comment lines
    line = line.replace('|'," ")
    nodes = map(int, line.split()[0:2])
    if nodes[0] not in store: store[nodes[0]] = Node(nodes[0])
    if nodes[1] not in store: store[nodes[1]] = Node(nodes[1])
    linkNodes(store[nodes[0]], store[nodes[1]])
  print "CAIDA AS-relation graph successfully imported, size {}".format(len(store))
  return store

def makeStoreASRelGraphMaxDeg(pathToGraph, degIdx=0):
  with open(pathToGraph, "r") as f:
    inData = f.readlines()
  store = dict()
  nodeDeg = dict()
  for line in inData:
    if line.strip()[0] == "#": continue # Skip comment lines
    line = line.replace('|'," ")
    nodes = map(int, line.split()[0:2])
    if nodes[0] not in nodeDeg: nodeDeg[nodes[0]] = 0
    if nodes[1] not in nodeDeg: nodeDeg[nodes[1]] = 0
    nodeDeg[nodes[0]] += 1
    nodeDeg[nodes[1]] += 1
  sortedNodes = sorted(nodeDeg.keys(), \
                       key=lambda x: (nodeDeg[x], x), \
                       reverse=True)
  maxDegNodeID = sortedNodes[degIdx]
  return makeStoreASRelGraphFixedRoot(pathToGraph, maxDegNodeID)

def makeStoreASRelGraphFixedRoot(pathToGraph, rootNodeID):
  with open(pathToGraph, "r") as f:
    inData = f.readlines()
  store = dict()
  for line in inData:
    if line.strip()[0] == "#": continue # Skip comment lines
    line = line.replace('|'," ")
    nodes = map(int, line.split()[0:2])
    if nodes[0] not in store:
      store[nodes[0]] = Node(nodes[0])
      if nodes[0] == rootNodeID: store[nodes[0]].info.treeID += 1000000000
    if nodes[1] not in store:
      store[nodes[1]] = Node(nodes[1])
      if nodes[1] == rootNodeID: store[nodes[1]].info.treeID += 1000000000
    linkNodes(store[nodes[0]], store[nodes[1]])
  print "CAIDA AS-relation graph successfully imported, size {}".format(len(store))
  return store

def makeStoreDimesEdges(pathToGraph, rootNodeID=None):
  # Read from a DIMES csv-formatted graph from a gzip file
  store = dict()
  with gzip.open(pathToGraph, "r") as f:
    inData = f.readlines()
  size = len(inData)
  index = 0
  for edge in inData:
    if not index % 1000:
      pct = 100.0*index/size
      print "Processing edge {}, {:.2f}%".format(index, pct)
    index += 1
    dat = edge.rstrip().split(',')
    node1 = "N" + str(dat[0].strip())
    node2 = "N" + str(dat[1].strip())
    if '?' in node1 or '?' in node2: continue #Unknown node
    if node1 == rootNodeID: node1 = "R" + str(dat[0].strip())
    if node2 == rootNodeID: node2 = "R" + str(dat[1].strip())
    if node1 not in store: store[node1] = Node(node1)
    if node2 not in store: store[node2] = Node(node2)
    if node1 != node2: linkNodes(store[node1], store[node2])
  print "DIMES graph successfully imported, size {}".format(len(store))
  return store

def makeStoreGeneratedGraph(pathToGraph, root=None):
  with open(pathToGraph, "r") as f:
    inData = f.readlines()
  store = dict()
  for line in inData:
    if line.strip()[0] == "#": continue # Skip comment lines
    nodes = map(int, line.strip().split(' ')[0:2])
    node1 = nodes[0]
    node2 = nodes[1]
    if node1 == root: node1 += 1000000
    if node2 == root: node2 += 1000000
    if node1 not in store: store[node1] = Node(node1)
    if node2 not in store: store[node2] = Node(node2)
    linkNodes(store[node1], store[node2])
  print "Generated graph successfully imported, size {}".format(len(store))
  return store


############################################
# Functions used as parts of network tests #
############################################

def idleUntilConverged(store):
  nodeIDs = sorted(store.keys())
  timeOfLastChange = 0
  step = 0
  # Idle until the network has converged
  while step - timeOfLastChange < 4*TIMEOUT:
    step += 1
    print "Step: {}, last change: {}".format(step, timeOfLastChange)
    changed = False
    for nodeID in nodeIDs:
      # Update node status, send messages
      changed |= store[nodeID].tick()
    for nodeID in nodeIDs:
      # Process messages
      changed |= store[nodeID].handleMessages()
    if changed: timeOfLastChange = step
  initTables(store)
  return store

def getCacheIndex(nodes, sourceIndex, destIndex):
  return sourceIndex*nodes + destIndex

def initTables(store):
  nodeIDs = sorted(store.keys())
  nNodes = len(nodeIDs)
  print "Initializing routing tables for {} nodes".format(nNodes)
  for idx in xrange(nNodes):
    nodeID = nodeIDs[idx]
    store[nodeID].initTable()
  print "Routing tables initialized"
  return None

def getCache(store):
  nodeIDs = sorted(store.keys())
  nNodes = len(nodeIDs)
  nodeIdxs = dict()
  for nodeIdx in xrange(nNodes):
    nodeIdxs[nodeIDs[nodeIdx]] = nodeIdx
  cache = array.array("H", [0]*nNodes*nNodes)
  for sourceIdx in xrange(nNodes):
    sourceID = nodeIDs[sourceIdx]
    print "Building fast lookup table for node {} / {} ({})".format(sourceIdx+1, nNodes, sourceID)
    for destIdx in xrange(nNodes):
      destID = nodeIDs[destIdx]
      if sourceID == destID: nextHop = destID # lookup would fail
      else: nextHop = store[sourceID].lookup(store[destID].info)
      nextHopIdx = nodeIdxs[nextHop]
      cache[getCacheIndex(nNodes, sourceIdx, destIdx)] = nextHopIdx
  return cache

def testPaths(store, dists):
  cache = getCache(store)
  nodeIDs = sorted(store.keys())
  nNodes = len(nodeIDs)
  idxs = dict()
  for nodeIdx in xrange(nNodes):
    nodeID = nodeIDs[nodeIdx]
    idxs[nodeID] = nodeIdx
  results = dict()
  for sourceIdx in xrange(nNodes):
    sourceID = nodeIDs[sourceIdx]
    print "Testing paths from node {} / {} ({})".format(sourceIdx+1, len(nodeIDs), sourceID)
    #dists = dijkstra(store, sourceID)
    for destIdx in xrange(nNodes):
      destID = nodeIDs[destIdx]
      if destID == sourceID: continue # Skip self
      distIdx = getCacheIndex(nNodes, sourceIdx, destIdx)
      eHops = dists[distIdx]
      if not eHops: continue # The network is split, no path exists
      hops = 0
      for pair in ((sourceIdx, destIdx), (destIdx, sourceIdx)): # Either direction because source routing
        nHops = 0
        locIdx = pair[0]
        dIdx = pair[1]
        while locIdx != dIdx:
          locIdx = cache[getCacheIndex(nNodes, locIdx, dIdx)]
          nHops += 1
        if not hops or nHops < hops: hops = nHops
      if eHops not in results: results[eHops] = dict()
      if hops not in results[eHops]: results[eHops][hops] = 0
      results[eHops][hops] += 1
  return results

def getAvgStretch(pathMatrix):
  avgStretch = 0.
  checked = 0.
  for eHops in sorted(pathMatrix.keys()):
    for nHops in sorted(pathMatrix[eHops].keys()):
      count = pathMatrix[eHops][nHops]
      stretch = float(nHops)/float(max(1, eHops))
      avgStretch += stretch*count
      checked += count
  avgStretch /= max(1, checked)
  return avgStretch

def getMaxStretch(pathMatrix):
  maxStretch = 0.
  for eHops in sorted(pathMatrix.keys()):
    for nHops in sorted(pathMatrix[eHops].keys()):
      stretch = float(nHops)/float(max(1, eHops))
      maxStretch = max(maxStretch, stretch)
  return maxStretch

def getCertSizes(store):
  # Returns nCerts frequency distribution
  # De-duplicates common certs (for shared prefixes in the path)
  sizes = dict()
  for node in store.values():
    certs = set()
    for peer in node.peers.values():
      pCerts = set()
      assert len(peer.path) == 2
      assert peer.coords[-1] == peer.path[0]
      hops = peer.coords + peer.path[1:]
      for hopIdx in xrange(len(hops)-1):
        send = hops[hopIdx]
        if send == node.info.nodeID: continue # We created it, already have it
        path = hops[0:hopIdx+2]
        # Each cert is signed by the sender
        # Includes information about the path from the sender to the next hop
        # Next hop is at hopIdx+1, so the path to next hop is hops[0:hopIdx+2]
        cert = "{}:{}".format(send, path)
        certs.add(cert)
    size = len(certs)
    if size not in sizes: sizes[size] = 0
    sizes[size] += 1
  return sizes

def getMinLinkCertSizes(store):
  # Returns nCerts frequency distribution
  # De-duplicates common certs (for shared prefixes in the path)
  # Based on the minimum number of certs that must be traded through a particular link
  # Handled per link
  sizes = dict()
  for node in store.values():
    peerCerts = dict()
    for peer in node.peers.values():
      pCerts = set()
      assert len(peer.path) == 2
      assert peer.coords[-1] == peer.path[0]
      hops = peer.coords + peer.path[1:]
      for hopIdx in xrange(len(hops)-1):
        send = hops[hopIdx]
        if send == node.info.nodeID: continue # We created it, already have it
        path = hops[0:hopIdx+2]
        # Each cert is signed by the sender
        # Includes information about the path from the sender to the next hop
        # Next hop is at hopIdx+1, so the path to next hop is hops[0:hopIdx+2]
        cert = "{}:{}".format(send, path)
        pCerts.add(cert)
      peerCerts[peer.nodeID] = pCerts
    for peer in peerCerts:
      size = 0
      pCerts = peerCerts[peer]
      for cert in pCerts:
        required = True
        for p2 in peerCerts:
          if p2 == peer: continue
          p2Certs = peerCerts[p2]
          if cert in p2Certs: required = False
        if required: size += 1
      if size not in sizes: sizes[size] = 0
      sizes[size] += 1
  return sizes

def getPathSizes(store):
  # Returns frequency distribution of the total number of hops the routing table
  # I.e. a node with 3 peers, each with 5 hop coord+path, would count as 3x5=15
  sizes = dict()
  for node in store.values():
    size = 0
    for peer in node.peers.values():
      assert len(peer.path) == 2
      assert peer.coords[-1] == peer.path[0]
      peerSize = len(peer.coords) + len(peer.path) - 1 # double-counts peer, -1
      size += peerSize
    if size not in sizes: sizes[size] = 0
    sizes[size] += 1
  return sizes

def getPeerSizes(store):
  # Returns frequency distribution of the number of peers each node has
  sizes = dict()
  for node in store.values():
    nPeers = len(node.peers)
    if nPeers not in sizes: sizes[nPeers] = 0
    sizes[nPeers] += 1
  return sizes

def getAvgSize(sizes):
  sumSizes = 0
  nNodes = 0
  for size in sizes:
    count = sizes[size]
    sumSizes += size*count
    nNodes += count
  avgSize = float(sumSizes)/max(1, nNodes)
  return avgSize

def getMaxSize(sizes):
  return max(sizes.keys())

def getMinSize(sizes):
  return min(sizes.keys())

def getResults(pathMatrix):
  results = []
  for eHops in sorted(pathMatrix.keys()):
    for nHops in sorted(pathMatrix[eHops].keys()):
      count = pathMatrix[eHops][nHops]
      results.append("{} {} {}".format(eHops, nHops, count))
  return '\n'.join(results)

####################################
# Functions to run different tests #
####################################

def runTest(store):
  # Runs the usual set of tests on the store
  # Does not save results, so only meant for quick tests
  # To e.g. check the code works, maybe warm up the pypy jit
  for node in store.values():
    node.info.time = random.randint(0, TIMEOUT)
    node.info.tstamp = TIMEOUT
  print "Begin testing network"
  dists = None
  if not dists: dists = dijkstrall(store)
  idleUntilConverged(store)
  pathMatrix = testPaths(store, dists)
  avgStretch = getAvgStretch(pathMatrix)
  maxStretch = getMaxStretch(pathMatrix)
  peers = getPeerSizes(store)
  certs = getCertSizes(store)
  paths = getPathSizes(store)
  linkCerts = getMinLinkCertSizes(store)
  avgPeerSize = getAvgSize(peers)
  maxPeerSize = getMaxSize(peers)
  avgCertSize = getAvgSize(certs)
  maxCertSize = getMaxSize(certs)
  avgPathSize = getAvgSize(paths)
  maxPathSize = getMaxSize(paths)
  avgLinkCert = getAvgSize(linkCerts)
  maxLinkCert = getMaxSize(linkCerts)
  totalCerts = sum(map(lambda x: x*certs[x], certs.keys()))
  totalLinks = sum(map(lambda x: x*peers[x], peers.keys())) # one-way links
  avgCertsPerLink = float(totalCerts)/max(1, totalLinks)
  print "Finished testing network"
  print "Avg / Max stretch: {} / {}".format(avgStretch, maxStretch)
  print "Avg / Max nPeers size: {} / {}".format(avgPeerSize, maxPeerSize)
  print "Avg / Max nCerts size: {} / {}".format(avgCertSize, maxCertSize)
  print "Avg / Max total hops in any node's routing table: {} / {}".format(avgPathSize, maxPathSize)
  print "Avg / Max lower bound cert requests per link (one-way): {} / {}".format(avgLinkCert, maxLinkCert)
  print "Avg certs per link (one-way): {}".format(avgCertsPerLink)
  return # End of function

def rootNodeASTest(path, outDir="output-treesim-AS", dists=None, proc = 1):
  # Checks performance for every possible choice of root node
  # Saves output for each root node to a separate file on disk
  # path = input path to some caida.org formatted AS-relationship graph
  if not os.path.exists(outDir): os.makedirs(outDir)
  assert os.path.exists(outDir)
  store = makeStoreASRelGraph(path)
  nodes = sorted(store.keys())
  for nodeIdx in xrange(len(nodes)):
    if nodeIdx % proc != 0: continue # Work belongs to someone else
    rootNodeID = nodes[nodeIdx]
    outpath = outDir+"/{}".format(rootNodeID)
    if os.path.exists(outpath):
      print "Skipping {}, already processed".format(rootNodeID)
      continue
    store = makeStoreASRelGraphFixedRoot(path, rootNodeID)
    for node in store.values():
      node.info.time = random.randint(0, TIMEOUT)
      node.info.tstamp = TIMEOUT
    print "Beginning {}, size {}".format(nodeIdx, len(store))
    if not dists: dists = dijkstrall(store)
    idleUntilConverged(store)
    pathMatrix = testPaths(store, dists)
    avgStretch = getAvgStretch(pathMatrix)
    maxStretch = getMaxStretch(pathMatrix)
    results = getResults(pathMatrix)
    with open(outpath, "w") as f:
      f.write(results)
    print "Finished test for root AS {} ({} / {})".format(rootNodeID, nodeIdx+1, len(store))
    print "Avg / Max stretch: {} / {}".format(avgStretch, maxStretch)
    #break # Stop after 1, because they can take forever
  return # End of function

def timelineASTest():
  # Meant to study the performance of the network as a function of network size
  # Loops over a set of AS-relationship graphs
  # Runs a test on each graph, selecting highest-degree node as the root
  # Saves results for each graph to a separate file on disk
  outDir = "output-treesim-timeline-AS"
  if not os.path.exists(outDir): os.makedirs(outDir)
  assert os.path.exists(outDir)
  paths = sorted(glob.glob("asrel/datasets/*"))
  for path in paths:
    date = os.path.basename(path).split(".")[0]
    outpath = outDir+"/{}".format(date)
    if os.path.exists(outpath):
      print "Skipping {}, already processed".format(date)
      continue
    store = makeStoreASRelGraphMaxDeg(path)
    dists = None
    for node in store.values():
      node.info.time = random.randint(0, TIMEOUT)
      node.info.tstamp = TIMEOUT
    print "Beginning {}, size {}".format(date, len(store))
    if not dists: dists = dijkstrall(store)
    idleUntilConverged(store)
    pathMatrix = testPaths(store, dists)
    avgStretch = getAvgStretch(pathMatrix)
    maxStretch = getMaxStretch(pathMatrix)
    results = getResults(pathMatrix)
    with open(outpath, "w") as f:
      f.write(results)
    print "Finished {} with {} nodes".format(date, len(store))
    print "Avg / Max stretch: {} / {}".format(avgStretch, maxStretch)
    #break # Stop after 1, because they can take forever
  return # End of function

def timelineDimesTest():
  # Meant to study the performance of the network as a function of network size
  # Loops over a set of AS-relationship graphs
  # Runs a test on each graph, selecting highest-degree node as the root
  # Saves results for each graph to a separate file on disk
  outDir = "output-treesim-timeline-dimes"
  if not os.path.exists(outDir): os.makedirs(outDir)
  assert os.path.exists(outDir)
  # Input files are named ASEdgesX_Y where X = month (no leading 0), Y = year
  paths = sorted(glob.glob("DIMES/ASEdges/*.gz"))
  exists = set(glob.glob(outDir+"/*"))
  for path in paths:
    date = os.path.basename(path).split(".")[0]
    outpath = outDir+"/{}".format(date)
    if outpath in exists:
      print "Skipping {}, already processed".format(date)
      continue
    store = makeStoreDimesEdges(path)
    # Get the highest degree node and make it root
    # Sorted by nodeID just to make it stable in the event of a tie
    nodeIDs = sorted(store.keys())
    bestRoot = ""
    bestDeg = 0
    for nodeID in nodeIDs:
      node = store[nodeID]
      if len(node.links) > bestDeg:
        bestRoot = nodeID
        bestDeg = len(node.links)
    assert bestRoot
    store = makeStoreDimesEdges(path, bestRoot)
    rootID = "R" + bestRoot[1:]
    assert rootID in store
    # Don't forget to set random seed before setitng times
    # To make results reproducible
    nodeIDs = sorted(store.keys())
    random.seed(12345)
    for nodeID in nodeIDs:
      node = store[nodeID]
      node.info.time = random.randint(0, TIMEOUT)
      node.info.tstamp = TIMEOUT
    print "Beginning {}, size {}".format(date, len(store))
    if not dists: dists = dijkstrall(store)
    idleUntilConverged(store)
    pathMatrix = testPaths(store, dists)
    avgStretch = getAvgStretch(pathMatrix)
    maxStretch = getMaxStretch(pathMatrix)
    results = getResults(pathMatrix)
    with open(outpath, "w") as f:
      f.write(results)
    print "Finished {} with {} nodes".format(date, len(store))
    print "Avg / Max stretch: {} / {}".format(avgStretch, maxStretch)
    break # Stop after 1, because they can take forever
  return # End of function

def scalingTest(maxTests=None, inputDir="graphs"):
  # Meant to study the performance of the network as a function of network size
  # Loops over a set of nodes in a previously generated graph
  # Runs a test on each graph, testing each node as the root
  # if maxTests is set, tests only that number of roots (highest degree first)
  # Saves results for each graph to a separate file on disk
  outDir = "output-treesim-{}".format(inputDir)
  if not os.path.exists(outDir): os.makedirs(outDir)
  assert os.path.exists(outDir)
  paths = sorted(glob.glob("{}/*".format(inputDir)))
  exists = set(glob.glob(outDir+"/*"))
  for path in paths:
    gc.collect() # pypy waits for gc to close files
    graph = os.path.basename(path).split(".")[0]
    store = makeStoreGeneratedGraph(path)
    # Get the highest degree node and make it root
    # Sorted by nodeID just to make it stable in the event of a tie
    nodeIDs = sorted(store.keys(), key=lambda x: len(store[x].links), reverse=True)
    dists = None
    if maxTests: nodeIDs = nodeIDs[:maxTests]
    for nodeID in nodeIDs:
      nodeIDStr = str(nodeID).zfill(len(str(len(store)-1)))
      outpath = outDir+"/{}-{}".format(graph, nodeIDStr)
      if outpath in exists:
        print "Skipping {}-{}, already processed".format(graph, nodeIDStr)
        continue
      store = makeStoreGeneratedGraph(path, nodeID)
      # Don't forget to set random seed before setting times
      random.seed(12345) # To make results reproducible
      nIDs = sorted(store.keys())
      for nID in nIDs:
        node = store[nID]
        node.info.time = random.randint(0, TIMEOUT)
        node.info.tstamp = TIMEOUT
      print "Beginning {}, size {}".format(graph, len(store))
      if not dists: dists = dijkstrall(store)
      idleUntilConverged(store)
      pathMatrix = testPaths(store, dists)
      avgStretch = getAvgStretch(pathMatrix)
      maxStretch = getMaxStretch(pathMatrix)
      results = getResults(pathMatrix)
      with open(outpath, "w") as f:
        f.write(results)
      print "Finished {} with {} nodes for root {}".format(graph, len(store), nodeID)
      print "Avg / Max stretch: {} / {}".format(avgStretch, maxStretch)
  return # End of function

##################
# Main Execution #
##################

if __name__ == "__main__":
  if True: # Run a quick test
    random.seed(12345) # DEBUG
    store = makeStoreSquareGrid(4)
    runTest(store) # Quick test
  store = None
  # Do some real work
  #runTest(makeStoreDimesEdges("DIMES/ASEdges/ASEdges1_2007.csv.gz"))
  #timelineDimesTest()
  #rootNodeASTest("asrel/datasets/19980101.as-rel.txt")
  #timelineASTest()
  #rootNodeASTest("hype-2016-09-19.list", "output-treesim-hype")
  #scalingTest(None, "graphs-20") # First argument 1 to only test 1 root per graph
  #store = makeStoreGeneratedGraph("bgp_tables")
  #store = makeStoreGeneratedGraph("skitter")
  #store = makeStoreASRelGraphMaxDeg("hype-2016-09-19.list") #http://hia.cjdns.ca/watchlist/c/walk.peers.20160919
  #store = makeStoreGeneratedGraph("fc00-2017-08-12.txt")
  if store: runTest(store)
  #rootNodeASTest("skitter", "output-treesim-skitter", None, 0, 1)
  #scalingTest(1, "graphs-20") # First argument 1 to only test 1 root per graph
  #scalingTest(1, "graphs-21") # First argument 1 to only test 1 root per graph
  #scalingTest(1, "graphs-22") # First argument 1 to only test 1 root per graph
  #scalingTest(1, "graphs-23") # First argument 1 to only test 1 root per graph
  if not store:
    import sys
    args = sys.argv
    if len(args) == 2:
      job_number = int(sys.argv[1])
      #rootNodeASTest("fc00-2017-08-12.txt", "fc00", None, job_number)
      #rootNodeASTest("skitter", "out-skitter", None, job_number)
      rootNodeASTest("walk-1517414401.txt.map", "out-walk", None, job_number)
    else:
      print "Usage: {} job_number".format(args[0])
      print "job_number = which job set to run on this node (1-indexed)"

