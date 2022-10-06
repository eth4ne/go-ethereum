import csv
import matplotlib as mpl
mpl.use('Agg')
import matplotlib.pyplot as plt
import numpy as np
import sys
from itertools import zip_longest
from pathlib import Path
from matplotlib.ticker import MaxNLocator
from datetime import datetime

# file paths
blockInfosLogFilePath = "./blockInfos/"
trieInspectsLogFilePath = "./trieInspects/"
graphPath = "./graphs/"

# options
simulationModeNames = ['ethereum', 'ethane', 'ethanos']
maxTickNum = 10

# make empty 2d list -> list[b][a]
def TwoD(a, b, isInt):
    if isInt:
        return np.zeros(a * b, dtype=int).reshape(b, a)
    else:
        return np.zeros(a * b, dtype=float).reshape(b, a)

# make the directory if not exist
def makeDir(path):
    Path(path).mkdir(parents=True, exist_ok=True)

# parse block infos log file
def parseBlockInfos(simulMode, startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion):

    # get log file name
    blockInfosLogFileName = ""
    if simulMode == 0:
        blockInfosLogFileName += "ethereum_simulate_block_infos_" + str(startBlockNum) + "_" + str(endBlockNum) + ".txt"
    elif simulMode == 1:
        blockInfosLogFileName += "ethane_simulate_block_infos_" + str(startBlockNum) + "_" + str(endBlockNum) \
            + "_" + str(deleteEpoch) + "_" + str(inactivateEpoch) + "_" + str(inactivateCriterion) + ".txt"
    elif simulMode == 2:
        blockInfosLogFileName += "ethanos_simulate_block_infos_" + str(startBlockNum) + "_" + str(endBlockNum) + "_" + str(inactivateCriterion) + ".txt"
    else:
        print("wrong mode:", simulMode)
        sys.exit()
    print("parsing block infos for simulation:", simulationModeNames[simulMode])

    # NodeStat.ToString() = totalNum, totalSize, fullNodeNum, fullNodeSize, shortNodeNum, shortNodeSize, leafNodeNum, leafNodeSize
    # logs[0~7][blockNum] = NewNodeStat.ToString()
    # logs[8~15][blockNum] = NewStorageNodeStat.ToString()
    # logs[16~23][blockNum] = TotalNodeStat.ToString()
    # logs[24~31][blockNum] = TotalStorageNodeStat.ToString()

    # logs[32][blockNum] = TimeToFlush (ns)
    # logs[33][blockNum] = TimeToDelete (ns)
    # logs[34][blockNum] = TimeToInactivate (ns)

    # RestoreStat.ToString() = RestorationNum, RestoredAccountNum, BloomFilterNum, MerkleProofNum, MerkleProofsSize, MerkleProofsNodesNum
    # logs[35~40][blockNum] = BlockRestoreStat.ToString()

    # logs[41][blockNum] = DeletedActiveNodeNum
    # logs[42][blockNum] = DeletedInactiveNodeNum
    # logs[43][blockNum] = InactivatedNodeNum

    # logs[44][blockNum] = ActiveAddressNum
    # logs[45][blockNum] = RestoredAddressNum
    # logs[46][blockNum] = CrumbAddressNum
    # logs[47][blockNum] = InactiveAddressNum

    columnNum = 100 # big enough value
    logs = TwoD(endBlockNum-startBlockNum+1, columnNum, True)

    # parse blockInfos log file
    f = open(blockInfosLogFilePath+blockInfosLogFileName, 'r')
    rdr = csv.reader(f)
    blockNum = 0
    for line in rdr:
        if len(line) == 0:
            continue

        params = line[0].split(" ")[:-1] # parsing
        params = [int(x) for x in params[3:]] # covert string to int (ignore first 3 items: activeTrieRoot, inactiveTrieRoot, blockNum)

        for i in range(len(params)):
            logs[i][blockNum] = params[i]

        blockNum += 1
        # if blockNum > 100000:
        #     break
    
    f.close()
    return logs

# parse trie inspects log file
def parseTrieInspects(simulMode, startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion):

    # get log file name
    trieInspectsLogFileName = ""
    if simulMode == 0:
        trieInspectsLogFileName += "ethereum_simulate_trie_inspects_" + str(startBlockNum) + "_" + str(endBlockNum) + "_" + str(inactivateCriterion) + ".txt"
    elif simulMode == 1:
        trieInspectsLogFileName += "ethane_simulate_trie_inspects_" + str(startBlockNum) + "_" + str(endBlockNum) \
            + "_" + str(deleteEpoch) + "_" + str(inactivateEpoch) + "_" + str(inactivateCriterion) + ".txt"
    elif simulMode == 2:
        trieInspectsLogFileName += "ethanos_simulate_trie_inspects_" + str(startBlockNum) + "_" + str(endBlockNum) + "_" + str(inactivateCriterion) + ".txt"
    else:
        print("wrong mode:", simulMode)
        sys.exit()

    # NodeStat.ToString() = totalNum, totalSize, fullNodeNum, fullNodeSize, shortNodeNum, shortNodeSize, leafNodeNum, leafNodeSize, minDepth, maxDepth, avgDepth
    # logs[0~10][blockNum] = active trie NodeStat.ToString()
    # logs[11~21][blockNum] = inactive trie NodeStat.ToString()

    columnNum = 100 # big enough value
    logs = TwoD(endBlockNum-startBlockNum+1, columnNum, True)

    # parse trie inspects log file
    f = open(trieInspectsLogFilePath+trieInspectsLogFileName, 'r')
    rdr = csv.reader(f)
    blockNums = []
    cnt = 0
    for line in rdr:
        if len(line) == 0:
            continue

        # parse line
        params = line[0].split(" ")[:-1]
        
        # get block num
        blockNum = int(params[0])
        blockNums.append(blockNum)

        # get node stats
        activeNodeStat = [int(x) for x in params[2:10]] + [float(x) for x in params[10:13]]
        inactiveNodeStat = [0, 0, 0, 0, 0, 0, 0, 0] + [0, 0, 0]
        if simulMode == 1:
            inactiveNodeStat = [int(x) for x in params[14:22]] + [float(x) for x in params[22:25]]

        # print("active node stat:", activeNodeStat)
        # print("inactive node stat:", inactiveNodeStat)
        params = activeNodeStat + inactiveNodeStat
        # print("params:", params)
        for i in range(len(params)):
            logs[i][cnt] = params[i]
        
        cnt += 1
        # if cnt > 100000:
        #     return

    f.close()
    return blockNums, logs

# draw stack plot for NodeStat
def drawNodeStatGraphs(xAxis, yAxis1, yAxis2, yAxis3, xLabelName, yLabelName, graphTitle):
    print("start drawing graph:", graphTitle)

    # set graph
    plt.figure()
    ax = plt.axes()

    # set title, label names
    plt.title(graphTitle, pad=10) # set graph title
    plt.xlabel(xLabelName, labelpad=10) # set x axis
    plt.ylabel(yLabelName, labelpad=10) # set y axis

    # set tick labels
    plt.ticklabel_format(style='sci', axis='x', scilimits=(0,0))
    plt.ticklabel_format(style='sci', axis='y', scilimits=(0,0))

    # select graph type
    # plt.plot(xAxis, yAxis1) # draw plot
    # plt.scatter(xAxis, yAxis1, s=1) # draw scatter graph
    plt.stackplot(xAxis, yAxis1, yAxis2, yAxis3, labels = ['full', 'short', 'leaf']) # draw stack graph
    plt.legend(loc='upper left')

    # set num of ticks
    ax.xaxis.set_major_locator(MaxNLocator(maxTickNum)) # set # of ticks
    ax.yaxis.set_major_locator(MaxNLocator(maxTickNum)) # set # of ticks

    # save graph
    makeDir(graphPath)
    graphName = graphTitle + ".png"
    plt.savefig(graphPath+graphName, format="png")

    print("  -> success\n")

# draw graphs for block infos log file
def drawGraphsForBlockInfos(simulMode, startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion):
    
    # parse log file
    logs = parseBlockInfos(simulMode, startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)

    #
    # draw graphs
    #
    
    # set x axis
    blockNums = list(range(startBlockNum,endBlockNum+1))
    blockNums = blockNums[:len(logs[0])]

    # set title
    if simulMode == 0:
        graphTitlePrefix = "ethereum "
        graphTitleSuffix = ""
    elif simulMode == 1:
        graphTitlePrefix = "ethane "
        graphTitleSuffix = " (de: " + str(deleteEpoch) + ", ie: " + str(inactivateEpoch) + ", ic: " + str(inactivateCriterion) + ")"
    elif simulMode == 2:
        graphTitlePrefix = "ethanos "
        graphTitleSuffix = " (ic: " + str(inactivateCriterion) + ")"
    else:
        print("wrong mode:", simulMode)
        sys.exit()
    
    # draw graph: NewNodeStat -> nums
    # title = 'new state trie node num'
    # drawNodeStatGraphs(blockNums, logs[2], logs[4], logs[6], 'block num', 'state trie node num', graphTitlePrefix+title)

    # draw graph: NewNodeStat -> sizes
    # title = 'new state trie node size'
    # drawNodeStatGraphs(blockNums, logs[3], logs[5], logs[7], 'block num', 'state trie node size (B)', graphTitlePrefix+title)

    # draw graph: TotalNodeStat -> nums
    title = graphTitlePrefix + 'archive state trie node num' + graphTitleSuffix
    drawNodeStatGraphs(blockNums, logs[18], logs[20], logs[22], 'block num', 'state trie node num', title)

    # draw graph: TotalNodeStat -> sizes
    title = graphTitlePrefix + 'archive state trie node size' + graphTitleSuffix
    drawNodeStatGraphs(blockNums, logs[19], logs[21], logs[23], 'block num', 'state trie node size (B)', title)

# draw graphs for block infos log file (compare simulation results)
def drawGraphsForBlockInfosCompare(startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion):

    # parse block infos logs
    blockInfosLogs = []
    for simulMode in range(3):
        blockInfosLogs.append(parseBlockInfos(simulMode, startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion))

    # set graph
    plt.figure()
    ax = plt.axes()

    # set title, label names
    plt.title("disk size (de:" + str(deleteEpoch) + ", ie:" + str(inactivateEpoch) + ", ic:" + str(inactivateCriterion) + ")", pad=10) # set graph title
    plt.xlabel("block number", labelpad=10) # set x axis
    plt.ylabel("archive state trie node size (B)", labelpad=10) # set y axis

    # set tick labels
    plt.ticklabel_format(style='sci', axis='x', scilimits=(0,0))
    plt.ticklabel_format(style='sci', axis='y', scilimits=(0,0))

    # draw lines
    for i in range(3):
        # set x axis
        blockNums = list(range(startBlockNum,endBlockNum+1))

        # select graph type
        plt.plot(blockNums, blockInfosLogs[i][17], label=simulationModeNames[i]) # draw plot
        plt.legend(loc='best')


    # set num of ticks
    ax.xaxis.set_major_locator(MaxNLocator(maxTickNum)) # set # of ticks
    ax.yaxis.set_major_locator(MaxNLocator(maxTickNum)) # set # of ticks

    # save graph
    makeDir(graphPath)
    graphName = "compare archive data (de:" + str(deleteEpoch) + ", ie:" + str(inactivateEpoch) + ", ic:" + str(inactivateCriterion) + ").png"
    plt.savefig(graphPath+graphName, format="png")

    print("drawing", graphName, "  -> success\n")



    #
    # print Ethane's block process time stats (flush, delete, inactivate)
    #

    flushTimes = []
    epochNum = 0

    if deleteEpoch != 1:
        for bn in blockNums:
            flushTime = blockInfosLogs[1][32][bn]
            deleteTime = blockInfosLogs[1][33][bn]
            inactivateTime = blockInfosLogs[1][34][bn]

            if (bn+1) % deleteEpoch == 0:
                print("at epoch", epochNum)
                print("avg flush time:", sum(flushTimes)/(deleteEpoch-1)/1000000, "ms")
                print(" max:", max(flushTimes)/1000000, "ms", " 90%:", np.percentile(flushTimes, 90)/1000000, " 75%:", np.percentile(flushTimes, 75)/1000000)
                print("  flush time:", flushTime/1000000, "ms")
                print("  delete time:", deleteTime/1000000, "ms")
                print("  inactivate time:", inactivateTime/1000000, "ms")
                print()

                # go to next epoch
                epochNum += 1
                flushTimes = []
            else:
                flushTimes.append(flushTime)



    #
    # Ethane's address stats (active, restored, crumb, inactive addresses)
    #

    # set graph
    plt.figure()
    ax = plt.axes()

    # set title, label names
    plt.title("Ethane's address ratio (de:" + str(deleteEpoch) + ", ie:" + str(inactivateEpoch) + ", ic:" + str(inactivateCriterion) + ")", pad=10) # set graph title
    plt.xlabel("block number", labelpad=10) # set x axis
    plt.ylabel("# of addresses", labelpad=10) # set y axis

    # set tick labels
    plt.ticklabel_format(style='sci', axis='x', scilimits=(0,0))
    plt.ticklabel_format(style='sci', axis='y', scilimits=(0,0))

    # select graph type
    plt.stackplot(blockNums, blockInfosLogs[1][44], blockInfosLogs[1][45], blockInfosLogs[1][46], blockInfosLogs[1][47], labels = ['active', 'restored', 'crumb', 'inactive']) # draw stack graph
    plt.legend(loc='upper left')

    # set num of ticks
    ax.xaxis.set_major_locator(MaxNLocator(maxTickNum)) # set # of ticks
    ax.yaxis.set_major_locator(MaxNLocator(maxTickNum)) # set # of ticks

    # save graph
    makeDir(graphPath)
    graphName = "ethane address ratio (de:" + str(deleteEpoch) + ", ie:" + str(inactivateEpoch) + ", ic:" + str(inactivateCriterion) + ")" + ".png"
    plt.savefig(graphPath+graphName, format="png")

    print("drawing", graphName, "  -> success\n")



    #
    # Restore stats (Ethane vs Ethanos)
    #

    # set graph
    plt.figure()
    ax = plt.axes()

    # set title, label names
    plt.title("compare avg restore proof size (de:" + str(deleteEpoch) + ", ie:" + str(inactivateEpoch) + ", ic:" + str(inactivateCriterion) + ")", pad=10) # set graph title
    plt.xlabel("block number", labelpad=10) # set x axis
    plt.ylabel("avg restore proof size (B)", labelpad=10) # set y axis

    # set tick labels
    plt.ticklabel_format(style='sci', axis='x', scilimits=(0,0))
    plt.ticklabel_format(style='sci', axis='y', scilimits=(0,0))

    # set x axis
    blockNums = list(range(startBlockNum,endBlockNum+1))

    # draw lines
    avgProofSize = [int(i/j) if j else 0 for i,j in zip(blockInfosLogs[1][39], blockInfosLogs[1][35])]
    plt.scatter(blockNums, avgProofSize, label=simulationModeNames[1], s=1) # draw plot
    avgProofSize = [int(i/j) if j else 0 for i,j in zip(blockInfosLogs[2][39], blockInfosLogs[2][35])]
    plt.scatter(blockNums, avgProofSize, label=simulationModeNames[2], s=0.1) # draw plot
    plt.legend(loc='best')
    
    # set num of ticks
    ax.xaxis.set_major_locator(MaxNLocator(maxTickNum)) # set # of ticks
    ax.yaxis.set_major_locator(MaxNLocator(maxTickNum)) # set # of ticks

    # save graph
    makeDir(graphPath)
    graphName = "compare avg restore proof size (de:" + str(deleteEpoch) + ", ie:" + str(inactivateEpoch) + ", ic:" + str(inactivateCriterion) + ").png"
    plt.savefig(graphPath+graphName, format="png")

    print("drawing", graphName, "  -> success\n")





# draw graphs for block infos log file
def drawGraphsForTrieInspects(simulMode, startBlockNum, endBlockNum, deleteEpoch=0, inactivateEpoch=0, inactivateCriterion=0):
    
    # get log file name
    trieInspectsLogFileName = ""
    if simulMode == 0:
        trieInspectsLogFileName += "ethereum_simulate_trie_inspects_" + str(startBlockNum) + "_" + str(endBlockNum) + "_" + str(inactivateCriterion) + ".txt"
    elif simulMode == 1:
        trieInspectsLogFileName += "ethane_simulate_trie_inspects_" + str(startBlockNum) + "_" + str(endBlockNum) \
        + "_" + str(deleteEpoch) + "_" + str(inactivateEpoch) + "_" + str(inactivateCriterion) + ".txt"
    elif simulMode == 2:
        trieInspectsLogFileName += "ethanos_simulate_trie_inspects_" + str(startBlockNum) + "_" + str(endBlockNum) + "_" + str(inactivateCriterion) + ".txt"
    else:
        print("wrong mode:", simulMode)
        sys.exit()

    # NodeStat.ToString() = totalNum, totalSize, fullNodeNum, fullNodeSize, shortNodeNum, shortNodeSize, leafNodeNum, leafNodeSize
    # logs[0~7][blockNum] = active trie NodeStat.ToString()
    # logs[8~15][blockNum] = inactive trie NodeStat.ToString()

    columnNum = 16
    logs = TwoD(endBlockNum-startBlockNum+1, columnNum, True)

    # parse trie inspects log file
    f = open(trieInspectsLogFilePath+trieInspectsLogFileName, 'r')
    rdr = csv.reader(f)
    blockNums = []
    cnt = 0
    for line in rdr:
        if len(line) == 0:
            continue

        # parse line
        params = line[0].split(" ")[:-1]
        
        # get block num
        blockNum = int(params[0])
        blockNums.append(blockNum)

        # get node stats
        activeNodeStat = [int(x) for x in params[2:10]] # covert string to int ()
        inactiveNodeStat = [0, 0, 0, 0, 0, 0, 0, 0]
        if simulMode == 1:
            inactiveNodeStat = [int(x) for x in params[12:20]] # covert string to int ()
        # print("active node stat:", activeNodeStat)
        # print("inactive node stat:", inactiveNodeStat)
        params = activeNodeStat + inactiveNodeStat
        # print("params:", params)
        for i in range(columnNum):
            logs[i][cnt] = params[i]

        cnt += 1
        # if cnt > 100000:
        #     return

    f.close()

    #
    # draw graphs
    #

    # set title
    if simulMode == 0:
        graphTitlePrefix = "ethereum "
        graphTitleSuffix = " (interval: " + str(inactivateCriterion) + ")"
    elif simulMode == 1:
        graphTitlePrefix = "ethane "
        graphTitleSuffix = " (de: " + str(deleteEpoch) + ", ie: " + str(inactivateEpoch) + ", ic: " + str(inactivateCriterion) + ")"
    elif simulMode == 2:
        graphTitlePrefix = "ethanos "
        graphTitleSuffix = " (ic: " + str(inactivateCriterion) + ")"
    else:
        print("wrong mode:", simulMode)
        sys.exit()

    # draw graph
    if simulMode == 0:
        # draw graph: state trie node nums
        title = graphTitlePrefix + 'state trie node num' + graphTitleSuffix
        drawNodeStatGraphs(blockNums, logs[2][:len(blockNums)], logs[4][:len(blockNums)], logs[6][:len(blockNums)], 'block num', 'state trie node num', title)

        # draw graph: state trie node sizes
        title = graphTitlePrefix + 'state trie node size' + graphTitleSuffix
        drawNodeStatGraphs(blockNums, logs[3][:len(blockNums)], logs[5][:len(blockNums)], logs[7][:len(blockNums)], 'block num', 'state trie node size (B)', title)

    elif simulMode == 1:
        # draw graph: max active current state trie node nums (at every checkpoint - 1 block)
        title = graphTitlePrefix + 'max state trie node num' + graphTitleSuffix
        print("blockNums:", blockNums[0::2])
        drawNodeStatGraphs(blockNums[0::2], logs[2][:len(blockNums)][0::2], logs[4][:len(blockNums)][0::2], logs[6][:len(blockNums)][0::2], 'block num', 'state trie node num', title)

        # draw graph: max active current state trie node sizes
        title = graphTitlePrefix + 'max state trie node size' + graphTitleSuffix
        drawNodeStatGraphs(blockNums[0::2], logs[3][:len(blockNums)][0::2], logs[5][:len(blockNums)][0::2], logs[7][:len(blockNums)][0::2], 'block num', 'state trie node size (B)', title)

        # draw graph: min active current state trie node nums (at every checkpoint block)
        title = graphTitlePrefix + 'min state trie node num' + graphTitleSuffix
        print("blockNums:", blockNums[1::2])
        drawNodeStatGraphs(blockNums[1::2], logs[2][:len(blockNums)][1::2], logs[4][:len(blockNums)][1::2], logs[6][:len(blockNums)][1::2], 'block num', 'state trie node num', title)

        # draw graph: min active current state trie node sizes
        title = graphTitlePrefix + 'min state trie node size' + graphTitleSuffix
        drawNodeStatGraphs(blockNums[1::2], logs[3][:len(blockNums)][1::2], logs[5][:len(blockNums)][1::2], logs[7][:len(blockNums)][1::2], 'block num', 'state trie node size (B)', title)

        # draw graph: inactive state trie node nums
        title = graphTitlePrefix + 'inactive state trie node num' + graphTitleSuffix
        drawNodeStatGraphs(blockNums, logs[10][:len(blockNums)], logs[12][:len(blockNums)], logs[14][:len(blockNums)], 'block num', 'state trie node num', title)

        # draw graph: inactive state trie node sizes
        title = graphTitlePrefix + 'inactive state trie node size' + graphTitleSuffix
        drawNodeStatGraphs(blockNums, logs[11][:len(blockNums)], logs[13][:len(blockNums)], logs[15][:len(blockNums)], 'block num', 'state trie node size (B)', title)

    elif simulMode == 2:
        # draw graph: cached state trie node nums
        title = graphTitlePrefix + 'cached state trie node num' + graphTitleSuffix
        drawNodeStatGraphs(blockNums, logs[2][:len(blockNums)], logs[4][:len(blockNums)], logs[6][:len(blockNums)], 'block num', 'state trie node num', title)

        # draw graph: cached state trie node sizes
        title = graphTitlePrefix + 'cached state trie node size' + graphTitleSuffix
        drawNodeStatGraphs(blockNums, logs[3][:len(blockNums)], logs[5][:len(blockNums)], logs[7][:len(blockNums)], 'block num', 'state trie node size (B)', title)

# draw graphs for trie inspects log file (compare simulation results)
def drawGraphsForTrieInspectsCompare(startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion):

    # parse trie inspects logs
    blockNumsList = []
    trieInspectsLogs = []
    for simulMode in range(3):
        blockNums, logs = parseTrieInspects(simulMode, startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)
        blockNumsList.append(blockNums)
        trieInspectsLogs.append(logs)

    # set graph
    plt.figure()
    ax = plt.axes()

    # set title, label names
    plt.title("state trie size (de:" + str(deleteEpoch) + ", ie:" + str(inactivateEpoch) + ", ic:" + str(inactivateCriterion) + ")", pad=10) # set graph title
    plt.xlabel("block number", labelpad=10) # set x axis
    plt.ylabel("state trie size (B)", labelpad=10) # set y axis

    # set tick labels
    plt.ticklabel_format(style='sci', axis='x', scilimits=(0,0))
    plt.ticklabel_format(style='sci', axis='y', scilimits=(0,0))

    # draw lines
    blockNums = blockNumsList[0]
    plt.plot(blockNums, trieInspectsLogs[0][1][:len(blockNums)], label='ethereum') # draw plot

    blockNums = blockNumsList[1]
    plt.plot(blockNums[0::2], trieInspectsLogs[1][1][:len(blockNums)][0::2], label='ethane active max') # draw plot
    plt.plot(blockNums[1::2], trieInspectsLogs[1][1][:len(blockNums)][1::2], label='ethane active min') # draw plot
    plt.plot(blockNums, trieInspectsLogs[1][12][:len(blockNums)], label='ethane inactive') # draw plot

    blockNums = blockNumsList[2]
    plt.plot(blockNums, trieInspectsLogs[2][1][:len(blockNums)], label='ethanos') # draw plot

    # set legend
    plt.legend(loc='best')

    # set num of ticks
    ax.xaxis.set_major_locator(MaxNLocator(maxTickNum)) # set # of ticks
    ax.yaxis.set_major_locator(MaxNLocator(maxTickNum)) # set # of ticks

    # save graph
    makeDir(graphPath)
    graphName = "compare state trie size (de:" + str(deleteEpoch) + ", ie:" + str(inactivateEpoch) + ", ic:" + str(inactivateCriterion) + ").png"
    plt.savefig(graphPath+graphName, format="png")

    print("  -> success\n")



# draw graphs for block infos log file (compare simulation results with various Ethane's params)
def drawGraphsForEthaneBlockInfosCompare(startBlockNum, endBlockNum, deleteEpochs, inactivateEpochs, inactivateCriterion):

    # parse block infos logs
    blockInfosLogs = []
    for i in range(len(deleteEpochs)):
        blockInfosLogs.append(parseBlockInfos(1, startBlockNum, endBlockNum, deleteEpochs[i], inactivateEpochs[i], inactivateCriterion))

    # set graph
    plt.figure()
    ax = plt.axes()

    # set title, label names
    plt.title("disk size (ic:" + str(inactivateCriterion) + ")", pad=10) # set graph title
    plt.xlabel("block number", labelpad=10) # set x axis
    plt.ylabel("archive state trie node size (B)", labelpad=10) # set y axis

    # set tick labels
    plt.ticklabel_format(style='sci', axis='x', scilimits=(0,0))
    plt.ticklabel_format(style='sci', axis='y', scilimits=(0,0))

    # set x axis
    blockNums = list(range(startBlockNum,endBlockNum+1))

    # draw Ethane lines
    for i in range(len(blockInfosLogs)):
        plt.plot(blockNums, blockInfosLogs[i][17], label="ethane (de: "+str(deleteEpochs[i])+", ie: "+str(inactivateEpochs[i]) +")") # draw plot
    # draw Ethereum line
    ethereumBlockInfosLog = parseBlockInfos(0, startBlockNum, endBlockNum, 0, 0, inactivateCriterion)
    plt.plot(blockNums, ethereumBlockInfosLog[17], label="ethereum")
    # draw Ethanos line
    ethanosBlockInfosLog = parseBlockInfos(2, startBlockNum, endBlockNum, 0, 0, inactivateCriterion)
    plt.plot(blockNums, ethanosBlockInfosLog[17], label="ethanos")

    # set legend
    plt.legend(loc='best')

    # set num of ticks
    ax.xaxis.set_major_locator(MaxNLocator(maxTickNum)) # set # of ticks
    ax.yaxis.set_major_locator(MaxNLocator(maxTickNum)) # set # of ticks

    # save graph
    makeDir(graphPath)
    graphName = "compare Ethane archive data (ic:" + str(inactivateCriterion) + ").png"
    plt.savefig(graphPath+graphName, format="png")

    print("drawing", graphName, "  -> success\n")



if __name__ == "__main__":
    print("start")
    startTime = datetime.now()

    # set simulation mode (0: original Ethereum, 1: Ethane, 2: Ethanos)
    simulationMode = 0
    # set simulation params
    startBlockNum = 0
    endBlockNum = 1000000
    deleteEpoch = 100
    inactivateEpoch = 100
    inactivateCriterion = 50000

    # drawGraphsForBlockInfosCompare(startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)
    # drawGraphsForTrieInspectsCompare(startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)
    # drawGraphsForEthaneBlockInfosCompare(startBlockNum, endBlockNum, [1,10,100], [1,10,100], inactivateCriterion)

    # draw graphs
    # drawGraphsForBlockInfos(simulationMode, startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)
    # drawGraphsForTrieInspects(simulationMode, startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)

    # draw graphs for all modes
    # for i in range(3):
    #     simulationMode = i
    #     drawGraphsForBlockInfos(simulationMode, startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)
    #     drawGraphsForTrieInspects(simulationMode, startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)

    print("end")
    endTime = datetime.now()
    print("elapsed time:", endTime-startTime)
