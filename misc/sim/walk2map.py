#!/usr/bin/env python2

def main():
  import sys
  args = sys.argv
  if len(args) != 2:
    print "Usage:", args[0], "path/to/walk.txt"
    return
  import glob
  files = glob.glob(args[1])
  if len(files) == 0:
    print "File not found:", args[1]
    return
  for inFile in files:
    with open(inFile, 'r') as f:  lines = f.readlines()
    out = []
    nodes = dict()
    for line in lines:
      words = line.strip().strip('[').strip(']').split(',')
      if len(words) < 5: continue
      if words[0].strip('"') != "link": continue
      first, second = words[3], words[4]
      if first not in nodes: nodes[first] = len(nodes)
      if second not in nodes: nodes[second] = len(nodes)
    for line in lines:
      words = line.strip().strip('[').strip(']').split(',')
      if len(words) < 5: continue
      if words[0].strip('"') != "link": continue
      first, second = nodes[words[3]], nodes[words[4]]
      out.append("{0} {1}".format(first, second))
    with open(inFile+".map", "w") as f: f.write("\n".join(out))
  # End loop over files
# End main

if __name__ == "__main__": main()
