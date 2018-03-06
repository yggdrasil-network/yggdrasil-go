import glob
import sys
inputDirPath =  sys.argv[1]

inputFilePaths = glob.glob(inputDirPath+"/*")
inputFilePaths.sort()

merged = dict()

stretches = []

total = 0
for inputFilePath in inputFilePaths:
  print "Processing file {}".format(inputFilePath)
  with open(inputFilePath, 'r') as f:
    inData = f.readlines()
  pathsChecked = 0.
  avgStretch = 0.
  for line in inData:
    dat = line.rstrip('\n').split(' ')
    eHops = int(dat[0])
    nHops = int(dat[1])
    count = int(dat[2])
    if eHops not in merged: merged[eHops] = dict()
    if nHops not in merged[eHops]: merged[eHops][nHops] = 0
    merged[eHops][nHops] += count
    total += count
    pathsChecked += count
    stretch = float(nHops)/eHops
    avgStretch += stretch*count
  finStretch = avgStretch / max(1, pathsChecked)
  stretches.append(str(finStretch))

hopsUsed = 0.
hopsNeeded = 0.
avgStretch = 0.
results = []
for eHops in sorted(merged.keys()):
  for nHops in sorted(merged[eHops].keys()):
    count = merged[eHops][nHops]
    result = "{} {} {}".format(eHops, nHops, count)
    results.append(result)
    hopsUsed += nHops*count
    hopsNeeded += eHops*count
    stretch = float(nHops)/eHops
    avgStretch += stretch*count
    print result
bandwidthUsage = hopsUsed/max(1, hopsNeeded)
avgStretch /= max(1, total)

with open("results.txt", "w") as f:
  f.write('\n'.join(results))

with open("stretches.txt", "w") as f:
  f.write('\n'.join(stretches))

print "Total files processed: {}".format(len(inputFilePaths))
print "Total paths found: {}".format(total)
print "Bandwidth usage: {}".format(bandwidthUsage)
print "Average stretch: {}".format(avgStretch)


