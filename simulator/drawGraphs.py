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
from statistics import median

# file paths
blockInfosLogFilePath = "./blockInfos/"
trieInspectsLogFilePath = "./trieInspects/"
txExecutionTimeLogFilePath = "./blockInfos/"
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

    # logs[32][blockNum] = BlockInterval (ns)
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

    # logs[48][blockNum] = BlockRestoreStat.MinProofSize
    # logs[49][blockNum] = BlockRestoreStat.MaxProofSize
    # logs[50][blockNum] = BlockRestoreStat.VoidMerkleProofNumAtMaxProof
    # logs[51][blockNum] = BlockRestoreStat.FirstEpochNumAtMaxProof

    # TODO(jmlee): add this later, this is temply has wrong values
    # logs[52][blockNum] = BlockRestoreStat.MaxVoidMerkleProofNum

    # logs[53~60][blockNum] = NewInactiveNodeStat.ToString()
    # logs[61~68][blockNum] = TotalInactiveNodeStat.ToString()

    # logs[69][blockNum] = TimeToFlushActive
    # logs[70][blockNum] = TimeToFlushInactive
    # logs[71][blockNum] = TimeToFlushStorage

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
    deleteTimes = []
    inactivateTimes = []
    epochNum = 1

    for bn in blockNums:
        blockInterval = blockInfosLogs[1][32][bn]
        deleteTime = blockInfosLogs[1][33][bn]
        inactivateTime = blockInfosLogs[1][34][bn]

        if (bn+1) % deleteEpoch == 0:
            deleteTimes.append(deleteTime)
            inactivateTimes.append(inactivateTime)
            if deleteEpoch == 1:
                flushTimes.append(blockInterval - deleteTime - inactivateTime)
        else:
            flushTimes.append(blockInterval)
        
        # show statistics
        if (bn+1) % inactivateCriterion == 0:
            # print("\nat epoch", epochNum)
            divScale = 1000000 # (10^3: microsecond, 10^6: millisecond, 10^9: second)

            # tex code to draw box plot
            print("\t% at epoch", epochNum)
            listsToDrawBoxPlot = [flushTimes, inactivateTimes, deleteTimes]
            for myList in listsToDrawBoxPlot:

                q1 = np.percentile(myList, 25)
                med = median(myList)
                q3 = np.percentile(myList, 75)
                iqr = q3 - q1

                myListWithoutOutliers = [x for x in myList if x >= q1 - 1.5*iqr]
                myListWithoutOutliers.sort()
                lowerWhisker = myListWithoutOutliers[0]

                myListWithoutOutliers = [x for x in myList if x <= q3 + 1.5*iqr]
                myListWithoutOutliers.sort()
                upperWhisker = myListWithoutOutliers[-1]
                
                print("\t\\addplot+ [boxplot prepared={")
                print("\t\tlower whisker=", round(lowerWhisker/divScale, 2), ", lower quartile=", round(q1/divScale, 2), \
                    ", median=", round(med/divScale, 2), ", upper quartile=", round(q3/divScale, 2), ", upper whisker=", round(upperWhisker/divScale, 2))
                print("\t}] coordinates {};")
            print("")

            # go to next epoch
            epochNum += 1
            flushTimes = []
            deleteTimes = []
            inactivateTimes = []



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

    # get statistics
    avgProofSizesEthane = []
    avgProofSizesEthanos = []
    restoredAccountNum = 0
    bloomFilterNum = 0
    merkleProofNum = 0
    MAX_PROOF_SIZE = 99999999999
    minProofSizeEthane = MAX_PROOF_SIZE
    minProofSizeEthanos = MAX_PROOF_SIZE
    maxProofSizeEthane = 0
    maxProofSizeEthanos = 0
    maxProofBlockNumEthane = 0
    maxProofBlockNumEthanos = 0
    restorationNumEthane = 0
    restorationNumEthanos = 0
    totalRestorationNumEthane = 0
    totalRestorationNumEthanos = 0
    merkleProofSizeEthane = 0
    merkleProofSizeEthanos = 0

    voidMerkleProofNumAtMaxProof = 0
    firstEpochNumAtMaxProof = 0


    epochNum = 1

    for bn in blockNums:

        # get avg restore proof size per block
        if (bn+1) % inactivateCriterion != 0:
            # for Ethane
            restorationNum = blockInfosLogs[1][35][bn]
            restorationNumEthane += restorationNum
            merkleProofsSize = blockInfosLogs[1][39][bn]
            merkleProofSizeEthane += merkleProofsSize
            minProofSize = blockInfosLogs[1][48][bn]
            maxProofSize = blockInfosLogs[1][49][bn]
            if restorationNum != 0:
                avgProofSize = int(merkleProofsSize/restorationNum)
                avgProofSizesEthane.append(avgProofSize)
                if minProofSizeEthane > minProofSize:
                    minProofSizeEthane = minProofSize
                if maxProofSizeEthane < maxProofSize:
                    maxProofSizeEthane = maxProofSize
                    maxProofBlockNumEthane = bn

            # for Ethanos
            restorationNum = blockInfosLogs[2][35][bn]
            restorationNumEthanos += restorationNum
            merkleProofsSize = blockInfosLogs[2][39][bn]
            merkleProofSizeEthanos += merkleProofsSize
            minProofSize = blockInfosLogs[2][48][bn]
            maxProofSize = blockInfosLogs[2][49][bn]
            if restorationNum != 0:
                avgProofSize = int(merkleProofsSize/restorationNum)
                avgProofSizesEthanos.append(avgProofSize)

                restoredAccountNum += blockInfosLogs[2][36][bn]
                bloomFilterNum += blockInfosLogs[2][37][bn]
                merkleProofNum += blockInfosLogs[2][38][bn]
                if minProofSizeEthanos > minProofSize:
                    minProofSizeEthanos = minProofSize
                if maxProofSizeEthanos < maxProofSize:
                    maxProofSizeEthanos = maxProofSize
                    maxProofBlockNumEthanos = bn
                    voidMerkleProofNumAtMaxProof = blockInfosLogs[2][50][bn]
                    firstEpochNumAtMaxProof = blockInfosLogs[2][51][bn]

        # show statistics
        else:
            # print("\nat epoch", epochNum)
            divScale = 1000 # (10^3: KB, 10^6: MB, 10^9: GB)

            # tex code to draw box plot
            print("\t% at epoch", epochNum)
            listsToDrawBoxPlot = [avgProofSizesEthane, avgProofSizesEthanos]
            if minProofSizeEthane == MAX_PROOF_SIZE:
                minProofSizeEthane = 0
            if minProofSizeEthanos == MAX_PROOF_SIZE:
                minProofSizeEthanos = 0
            minProofSizes = [minProofSizeEthane, minProofSizeEthanos]
            maxProofSizes = [maxProofSizeEthane, maxProofSizeEthanos]
            restoreNums = [restorationNumEthane, restorationNumEthanos]
            totalRestorationNumEthane += restorationNumEthane
            totalRestorationNumEthanos += restorationNumEthanos
            merkleProofSizes = [merkleProofSizeEthane, merkleProofSizeEthanos]
            maxProofBlockNums = [maxProofBlockNumEthane, maxProofBlockNumEthanos]
            index = 0
            for myList in listsToDrawBoxPlot:

                if len(myList) == 0:
                    myList = [0]
                
                # min = np.percentile(myList, 0)
                min = minProofSizes[index]
                q1 = np.percentile(myList, 25)
                # med = median(myList)
                if restoreNums[index] != 0:
                    avg = merkleProofSizes[index]/restoreNums[index]
                else:
                    avg = 0
                q3 = np.percentile(myList, 75)
                iqr = q3 - q1
                # max = np.percentile(myList, 100)
                max = maxProofSizes[index]
                
                myListWithoutOutliers = [x for x in myList if x >= q1 - 1.5*iqr]
                myListWithoutOutliers.sort()
                lowerWhisker = myListWithoutOutliers[0]

                myListWithoutOutliers = [x for x in myList if x <= q3 + 1.5*iqr]
                myListWithoutOutliers.sort()
                upperWhisker = myListWithoutOutliers[-1]

                print("\t\\addplot+ [boxplot prepared={")
                # print("\t\tlower whisker=", round(min/divScale, 2), ", lower quartile=", round(q1/divScale, 2), \
                #     ", median=", round(med/divScale, 2), ", upper quartile=", round(q3/divScale, 2), ", upper whisker=", round(max/divScale, 2))
                print("\t\tlower whisker=", round(min/divScale, 2), ", lower quartile=", round(avg/divScale, 2)-0.01, \
                    ", median=", round(avg/divScale, 2), ", upper quartile=", round(avg/divScale, 2)+0.01, ", upper whisker=", round(max/divScale, 2))
                print("\t}] coordinates {};")
                print("\t% => lower whisker:", round(lowerWhisker/divScale, 2), "/ upper whisker:", round(upperWhisker/divScale, 2))
                print("\t% => min:", round(min/divScale, 2), "/ max:", round(max/divScale, 2))
                print("\t% => # of restoration in this epoch:", restoreNums[index])
                print("\t% => bn with max proof:", maxProofBlockNums[index])
                if index == 1:
                    print("\t% => void Merkle Proof Num At Max Proof:", voidMerkleProofNumAtMaxProof)
                    print("\t% => first Epoch Num At Max Proof:", firstEpochNumAtMaxProof)
                index += 1
            
            # Ethanos void proof ratio: bloom filter vs merkle proof
            voidMerkleProofNum = merkleProofNum - restoredAccountNum
            if bloomFilterNum != 0 or voidMerkleProofNum != 0:
                print("\t% => bloom filter num:", bloomFilterNum, "/ void merkle proof num:", voidMerkleProofNum, "(", round(bloomFilterNum/(bloomFilterNum+voidMerkleProofNum)*100, 2), "% )")
            else:
                print("\t% => bloom filter num:", bloomFilterNum, "/ void merkle proof num:", voidMerkleProofNum, "(no void proof)")

            print("")
            # go to next epoch
            epochNum += 1
            avgProofSizesEthane = []
            avgProofSizesEthanos = []
            restoredAccountNum = 0
            bloomFilterNum = 0
            merkleProofNum = 0
            minProofSizeEthane = MAX_PROOF_SIZE
            minProofSizeEthanos = MAX_PROOF_SIZE
            maxProofSizeEthane = 0
            maxProofSizeEthanos = 0
            maxProofBlockNumEthane = 0
            maxProofBlockNumEthanos = 0
            restorationNumEthane = 0
            restorationNumEthanos = 0
            merkleProofSizeEthane = 0
            merkleProofSizeEthanos = 0
            voidMerkleProofNumAtMaxProof = 0
            firstEpochNumAtMaxProof = 0
    
    print("total restore num ethane:", totalRestorationNumEthane)
    print("total restore num ethanos:", totalRestorationNumEthanos)



# draw graphs for execution time of ethane's deletion/inactivation
def drawGraphsForEthaneDeletionInactivationTime(startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion):

    # parse block infos logs
    blockInfosLog = parseBlockInfos(1, startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)

    deleteTimes = []
    inactivateTimes = []
    epochNum = 1
    drawTukeyStyle = False # remove outliers following Tukey style box plot
    removeExtremeOutliers = True # remove outliers over 99%th
    if drawTukeyStyle and removeExtremeOutliers:
        print("do not set both options true")
        sys.exit()

    # endBlockNum=9000000

    blockNums = list(range(startBlockNum,endBlockNum+1))
    for bn in blockNums:
        deleteTime = blockInfosLog[33][bn]
        inactivateTime = blockInfosLog[34][bn]

        if (bn+1) % deleteEpoch == 0:
            deleteTimes.append(deleteTime)
        if (bn+1) % inactivateEpoch == 0:
            inactivateTimes.append(inactivateTime)

        # show statistics
        if (bn+1) % inactivateCriterion == 0:
            # print("\nat epoch", epochNum)
            divScale = 1000000 # (10^0: nanosecond, 10^3: microsecond, 10^6: millisecond, 10^9: second)

            # tex code to draw box plot
            print("\t% at epoch", epochNum)
            listsToDrawBoxPlot = [inactivateTimes, deleteTimes]
            for myList in listsToDrawBoxPlot:

                q1 = np.percentile(myList, 25)
                med = median(myList)
                q3 = np.percentile(myList, 75)
                iqr = q3 - q1

                # remove outliters ()
                myList.sort()
                myListLenWithOutliers = len(myList)
                if drawTukeyStyle:
                    myList = [x for x in myList if x >= q1 - 1.5*iqr and x <= q3 + 1.5*iqr]
                elif removeExtremeOutliers:
                    outlierNum = int(len(myList)*1/100)
                    myList = myList[0:-outlierNum]
                print("\t% outliers num:", myListLenWithOutliers - len(myList))
                lowerWhisker = myList[0]
                upperWhisker = myList[-1]

                print("\t\\addplot+ [boxplot prepared={")
                print("\t\tlower whisker=", round(lowerWhisker/divScale, 2), ", lower quartile=", round(q1/divScale, 2), \
                    ", median=", round(med/divScale, 2), ", upper quartile=", round(q3/divScale, 2), ", upper whisker=", round(upperWhisker/divScale, 2))
                print("\t}] coordinates {};")
            print("\t\\addplot+ [boxplot prepared={}] coordinates {};")
            print("")

            # go to next epoch
            epochNum += 1
            deleteTimes = []
            inactivateTimes = []



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
    plt.plot(blockNums[1::2], trieInspectsLogs[1][12][:len(blockNums)][1::2], label='ethane inactive max') # draw plot

    blockNums = blockNumsList[2]
    plt.plot(blockNums, trieInspectsLogs[2][1][:len(blockNums)], label='ethanos max') # draw plot

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



def analyzeTxExecuteTime(startBlockNum, endBlockNum):
    print("reading tx execute time logs...")

    # parse blockInfos log file
    txExecuteTimeLogFileName = "tx_execute_time_" + str(startBlockNum) + "_" + str(endBlockNum) + "_castor.txt"
    f = open(txExecutionTimeLogFilePath+txExecuteTimeLogFileName, 'r')
    rdr = csv.reader(f)

    # genesis block has no tx, so just put 0s
    blockTxNums = [0]
    blockTxExecuteTimes_CpuTime = [0]
    blockTxExecuteTimes_NowSince = [0]

    for params in rdr:
        # params: [blockNumber, 
        #   txNums -> deploy, transferEoA, transferCA, contractCall, 
        #   txExecutionTime_CpuTime -> deploy, transferEoA, transferCA, contractCall, 
        #   txExecutionTime_NowSince -> deploy, transferEoA, transferCA, contractCall]

        params = params[:-1]
        params = [int(x) for x in params]

        blockNum = params[0]
        blockTxNum = params[1] + params[2] + params[3] + params[4]
        blockTxExecuteTime_CpuTime = params[5] + params[6] + params[7] + params[8]
        blockTxExecuteTime_NowSince = params[9] + params[10] + params[11] + params[12]
        # print("blockNum:", blockNum)
        # print("blockTxNum:", blockTxNum)
        # print("blockTxExecuteTime_CpuTime:", blockTxExecuteTime_CpuTime)
        # print("blockTxExecuteTime_NowSince:", blockTxExecuteTime_NowSince)
        # print("")

        blockTxNums.append(blockTxNum)
        blockTxExecuteTimes_CpuTime.append(blockTxExecuteTime_CpuTime)
        blockTxExecuteTimes_NowSince.append(blockTxExecuteTime_NowSince)
    
    return blockTxExecuteTimes_NowSince



# compare restore proof size of Ethane and Ethanos, showing min, avg, max (ignore q1, q3)
def drawGraphsForRestorationOverhead(startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion):

    #
    # Restore stats (Ethane vs Ethanos)
    #
    
    ethaneBlockInfosLog = parseBlockInfos(1, startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)
    ethanosBlockInfosLog = parseBlockInfos(2, startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)

    # get statistics
    avgProofSizesEthane = []
    avgProofSizesEthanos = []
    restoredAccountNum = 0
    bloomFilterNum = 0
    merkleProofNum = 0
    MAX_PROOF_SIZE = 99999999999
    minProofSizeEthane = MAX_PROOF_SIZE
    minProofSizeEthanos = MAX_PROOF_SIZE
    maxProofSizeEthane = 0
    maxProofSizeEthanos = 0
    maxProofBlockNumEthane = 0
    maxProofBlockNumEthanos = 0
    restorationNumEthane = 0
    restorationNumEthanos = 0
    totalRestorationNumEthane = 0
    totalRestorationNumEthanos = 0
    merkleProofSizeEthane = 0
    merkleProofSizeEthanos = 0

    voidMerkleProofNumAtMaxProof = 0
    firstEpochNumAtMaxProof = 0

    epochNum = 1

    for bn in list(range(startBlockNum,endBlockNum+1)):

        # get avg restore proof size per block
        if (bn+1) % inactivateCriterion != 0:
            # for Ethane
            restorationNum = ethaneBlockInfosLog[35][bn]
            restorationNumEthane += restorationNum
            merkleProofsSize = ethaneBlockInfosLog[39][bn]
            merkleProofSizeEthane += merkleProofsSize
            minProofSize = ethaneBlockInfosLog[48][bn]
            maxProofSize = ethaneBlockInfosLog[49][bn]
            if restorationNum != 0:
                avgProofSize = int(merkleProofsSize/restorationNum)
                avgProofSizesEthane.append(avgProofSize)
                if minProofSizeEthane > minProofSize:
                    minProofSizeEthane = minProofSize
                if maxProofSizeEthane < maxProofSize:
                    maxProofSizeEthane = maxProofSize
                    maxProofBlockNumEthane = bn

            # for Ethanos
            restorationNum = ethanosBlockInfosLog[35][bn]
            restorationNumEthanos += restorationNum
            merkleProofsSize = ethanosBlockInfosLog[39][bn]
            merkleProofSizeEthanos += merkleProofsSize
            minProofSize = ethanosBlockInfosLog[48][bn]
            maxProofSize = ethanosBlockInfosLog[49][bn]
            if restorationNum != 0:
                avgProofSize = int(merkleProofsSize/restorationNum)
                avgProofSizesEthanos.append(avgProofSize)

                restoredAccountNum += ethanosBlockInfosLog[36][bn]
                bloomFilterNum += ethanosBlockInfosLog[37][bn]
                merkleProofNum += ethanosBlockInfosLog[38][bn]
                if minProofSizeEthanos > minProofSize:
                    minProofSizeEthanos = minProofSize
                if maxProofSizeEthanos < maxProofSize:
                    maxProofSizeEthanos = maxProofSize
                    maxProofBlockNumEthanos = bn
                    voidMerkleProofNumAtMaxProof = ethanosBlockInfosLog[50][bn]
                    firstEpochNumAtMaxProof = ethanosBlockInfosLog[51][bn]

        # show statistics
        else:
            # print("\nat epoch", epochNum)
            divScale = 1000 # (10^3: KB, 10^6: MB, 10^9: GB)

            # tex code to draw box plot
            print("\t% at epoch", epochNum)
            listsToDrawBoxPlot = [avgProofSizesEthane, avgProofSizesEthanos]
            if minProofSizeEthane == MAX_PROOF_SIZE:
                minProofSizeEthane = 0
            if minProofSizeEthanos == MAX_PROOF_SIZE:
                minProofSizeEthanos = 0
            minProofSizes = [minProofSizeEthane, minProofSizeEthanos]
            maxProofSizes = [maxProofSizeEthane, maxProofSizeEthanos]
            restoreNums = [restorationNumEthane, restorationNumEthanos]
            totalRestorationNumEthane += restorationNumEthane
            totalRestorationNumEthanos += restorationNumEthanos
            merkleProofSizes = [merkleProofSizeEthane, merkleProofSizeEthanos]
            maxProofBlockNums = [maxProofBlockNumEthane, maxProofBlockNumEthanos]
            index = 0
            digitsNum = 3
            for myList in listsToDrawBoxPlot:

                if len(myList) == 0:
                    myList = [0]
                
                # min = np.percentile(myList, 0)
                min = minProofSizes[index]
                q1 = np.percentile(myList, 25)
                med = median(myList)
                if restoreNums[index] != 0:
                    avg = merkleProofSizes[index]/restoreNums[index]
                else:
                    avg = 0
                q3 = np.percentile(myList, 75)
                iqr = q3 - q1
                # max = np.percentile(myList, 100)
                max = maxProofSizes[index]

                myListWithoutOutliers = [x for x in myList if x >= q1 - 1.5*iqr]
                myListWithoutOutliers.sort()
                lowerWhisker = myListWithoutOutliers[0]

                myListWithoutOutliers = [x for x in myList if x <= q3 + 1.5*iqr]
                myListWithoutOutliers.sort()
                upperWhisker = myListWithoutOutliers[-1]

                print("\t\\addplot+ [boxplot prepared={")
                print("\t\tlower whisker=", round(min/divScale, digitsNum), ", lower quartile=", round(avg/divScale - 0.001, digitsNum), \
                    ", median=", round(avg/divScale, digitsNum), ", upper quartile=", round(avg/divScale + 0.001, digitsNum), ", upper whisker=", round(max/divScale, digitsNum))
                print("\t}] coordinates {};")
                print("\t% => min:", round(min/divScale, digitsNum), "/ avg:", round(avg/divScale, digitsNum), "/ med:", round(med/divScale, digitsNum), "/ max:", round(max/divScale, digitsNum))
                print("\t% => lower whisker:", round(lowerWhisker/divScale, digitsNum), "/ upper whisker:", round(upperWhisker/divScale, digitsNum))
                print("\t% => # of restoration in this epoch:", restoreNums[index])
                print("\t% => block number having max proof:", maxProofBlockNums[index])
                if index == 1:
                    print("\t% => (for Ethanos)")
                    print("\t% => void Merkle Proof Num At Max Proof:", voidMerkleProofNumAtMaxProof)
                    print("\t% => first Epoch Num At Max Proof:", firstEpochNumAtMaxProof)
                index += 1

            # Ethanos void proof ratio: bloom filter vs merkle proof
            voidMerkleProofNum = merkleProofNum - restoredAccountNum
            if bloomFilterNum != 0 or voidMerkleProofNum != 0:
                print("\t% => bloom filter num:", bloomFilterNum, "/ void merkle proof num:", voidMerkleProofNum, "(", round(bloomFilterNum/(bloomFilterNum+voidMerkleProofNum)*100, 2), "% )")
            else:
                print("\t% => bloom filter num:", bloomFilterNum, "/ void merkle proof num:", voidMerkleProofNum, "(no void proof)")

            print("")
            # go to next epoch
            epochNum += 1
            avgProofSizesEthane = []
            avgProofSizesEthanos = []
            restoredAccountNum = 0
            bloomFilterNum = 0
            merkleProofNum = 0
            minProofSizeEthane = MAX_PROOF_SIZE
            minProofSizeEthanos = MAX_PROOF_SIZE
            maxProofSizeEthane = 0
            maxProofSizeEthanos = 0
            maxProofBlockNumEthane = 0
            maxProofBlockNumEthanos = 0
            restorationNumEthane = 0
            restorationNumEthanos = 0
            merkleProofSizeEthane = 0
            merkleProofSizeEthanos = 0
            voidMerkleProofNumAtMaxProof = 0
            firstEpochNumAtMaxProof = 0

    print("total restore num ethane:", totalRestorationNumEthane)
    print("total restore num ethanos:", totalRestorationNumEthanos)



# draw tex grasph for restore proof size of Ethane or Ethanos, showing min, avg, max (ignore q1, q3)
def drawGraphsForRestorationOverheadOfMode(simulationMode, startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion):

    #
    # Restore stats for Ethane or Ethanos
    #

    blockInfosLog = parseBlockInfos(simulationMode, startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)

    #
    # get statistics (*Epoch: stat of current epoch, *Total: stat of total blocks)
    #
    # # of restorations
    restorationNumEpoch = 0
    restorationNumTotal = 0
    # # of restored accounts
    restoredAccountNumEpoch = 0
    restoredAccountNumTotal = 0
    # # of bloom filters
    bloomFilterNumEpoch = 0
    bloomFilterNumTotal = 0
    # # of merkle proofs
    merkleProofNumEpoch = 0
    merkleProofNumTotal = 0
    # min restore proof size
    MAX_PROOF_SIZE = 99999999999 # constant representing initial min value
    minProofSizeEpoch = MAX_PROOF_SIZE
    # max restore proof stats (size, when it appeared, void merkle proofs num, first epoch num to restore)
    maxProofSizeEpoch = 0
    maxProofBlockNumEpoch = 0
    voidMerkleProofNumAtMaxProof = 0
    firstEpochNumAtMaxProof = 0
    # merkle proof size (including both membership and non-membership Merkle proofs)
    merkleProofSizeEpoch = 0
    merkleProofSizeTotal = 0
    # # of non-membership Merkle proofs
    voidMerkleProofNumEpoch = 0
    voidMerkleProofNumTotal = 0

    epochNum = 1

    for bn in list(range(startBlockNum,endBlockNum+1)):

        # for Ethane or Ethanos
        restorationNum = blockInfosLog[35][bn]
        if restorationNum != 0:
            restorationNumEpoch += restorationNum

            merkleProofsSize = blockInfosLog[39][bn]
            merkleProofSizeEpoch += merkleProofsSize
            minProofSize = blockInfosLog[48][bn]
            maxProofSize = blockInfosLog[49][bn]

            restoredAccountNumEpoch += blockInfosLog[36][bn]
            bloomFilterNumEpoch += blockInfosLog[37][bn]
            merkleProofNumEpoch += blockInfosLog[38][bn]
            if minProofSizeEpoch > minProofSize:
                minProofSizeEpoch = minProofSize
            if maxProofSizeEpoch < maxProofSize:
                maxProofSizeEpoch = maxProofSize
                maxProofBlockNumEpoch = bn
                voidMerkleProofNumAtMaxProof = blockInfosLog[50][bn]
                firstEpochNumAtMaxProof = blockInfosLog[51][bn]



        # show statistics of this epoch
        if (bn+1) % inactivateCriterion == 0:

            # min, avg, max
            if minProofSizeEpoch == MAX_PROOF_SIZE:
                minProofSizeEpoch = 0
            min = minProofSizeEpoch
            if restorationNumEpoch != 0:
                avg = merkleProofSizeEpoch/restorationNumEpoch
            else:
                avg = 0
            max = maxProofSizeEpoch

            # tex code to draw box plot
            digitsNum = 3
            divScale = 1000 # (10^3: KB, 10^6: MB, 10^9: GB)
            print("\t% at epoch", epochNum)
            print("\t\\addplot+ [boxplot prepared={")
            print("\t\tlower whisker=", round(min/divScale, digitsNum), ", lower quartile=", round(avg/divScale - 0.001, digitsNum), \
                ", median=", round(avg/divScale, digitsNum), ", upper quartile=", round(avg/divScale + 0.001, digitsNum), ", upper whisker=", round(max/divScale, digitsNum))
            print("\t}] coordinates {};")
            print("\t% => min:", round(min/divScale, digitsNum), "/ avg:", round(avg/divScale, digitsNum), "/ max:", round(max/divScale, digitsNum))
            print("\t% => # of restoration in this epoch:", restorationNumEpoch)
            print("\t% => block number having max proof:", maxProofBlockNumEpoch)
            if simulationMode == 2:
                # Ethanos void proof ratio: bloom filter vs non-membership merkle proof
                print("\t% => void Merkle Proof Num At Max Proof:", voidMerkleProofNumAtMaxProof)
                print("\t% => first Epoch Num At Max Proof (epoch starts from 1):", firstEpochNumAtMaxProof+1) # (epochNums in log files start from 0, but roundNums in latex graphs start from 1)
                voidMerkleProofNumEpoch = merkleProofNumEpoch - restoredAccountNumEpoch
                if bloomFilterNumEpoch != 0 or voidMerkleProofNumEpoch != 0:
                    print("\t% => bloom filter num:", bloomFilterNumEpoch, "/ void merkle proof num:", voidMerkleProofNumEpoch, "(", round(bloomFilterNumEpoch/(bloomFilterNumEpoch+voidMerkleProofNumEpoch)*100, 2), "% )")
                else:
                    print("\t% => bloom filter num:", bloomFilterNumEpoch, "/ void merkle proof num:", voidMerkleProofNumEpoch, "(no void proof)")
            print("")

            # update total stats
            restorationNumTotal += restorationNumEpoch
            restoredAccountNumTotal += restoredAccountNumEpoch
            bloomFilterNumTotal += bloomFilterNumEpoch
            merkleProofNumTotal += merkleProofNumEpoch
            merkleProofSizeTotal += merkleProofSizeEpoch
            voidMerkleProofNumTotal += voidMerkleProofNumEpoch

            # go to next epoch & init epoch stats
            epochNum += 1
            avgProofSizesEthanos = []
            restoredAccountNumEpoch = 0
            bloomFilterNumEpoch = 0
            merkleProofNumEpoch = 0
            minProofSizeEpoch = MAX_PROOF_SIZE
            maxProofSizeEpoch = 0
            maxProofBlockNumEpoch = 0
            restorationNumEpoch = 0
            merkleProofSizeEpoch = 0
            voidMerkleProofNumAtMaxProof = 0
            firstEpochNumAtMaxProof = 0

    print("print total stats")
    print("total restore num:", restorationNumTotal)
    print("total restored accs:", restoredAccountNumTotal)
    print("total merkle proofs num:", merkleProofNumTotal)
    print("total merkle proofs size:", merkleProofSizeTotal)
    if simulationMode == 2:
        print("total bloom filters num:", bloomFilterNumTotal)
        print("total non-membership Merkle proofs num:", voidMerkleProofNumTotal)
        print("  => bloom filter's false positive prob:", round(voidMerkleProofNumTotal/(bloomFilterNumTotal+voidMerkleProofNumTotal)*100, 2), "%")

    # check correctness
    if restorationNumTotal != restoredAccountNumTotal:
        print("ERROR: this cannot happen in conservative restoration")
    if merkleProofNumTotal != restoredAccountNumTotal+voidMerkleProofNumTotal:
        print("ERROR: Merkle proofs num is weird")



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

    # drawGraphsForEthaneDeletionInactivationTime(startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)
    # analyzeTxExecuteTime(startBlockNum, endBlockNum)
    # drawGraphsForRestorationOverhead(startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)
    # drawGraphsForRestorationOverheadOfMode(simulationMode, startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)

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
