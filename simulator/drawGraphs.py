import csv
import matplotlib as mpl
mpl.use('Agg')
import matplotlib.pyplot as plt
import numpy as np
import sys
from itertools import zip_longest
from pathlib import Path
from matplotlib.ticker import MaxNLocator

# file paths
blockInfosLogFilePath = "./blockInfos/"
trieInspectsLogFilePath = "./trieInspects/"
graphPath = "./graphs/"

# options
simulationMode = 0 # (0: original ethereum, 1: Ethane, 2: Ethanos)
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
def drawGraphsForBlockInfos(startBlockNum, endBlockNum, deleteEpoch=0, inactivateEpoch=0, inactivateCriterion=0):
    
    # get log file name
    blockInfosLogFileName = ""
    if simulationMode == 0:
        blockInfosLogFileName += "ethereum_simulate_block_infos_" + str(startBlockNum) + "_" + str(endBlockNum) + ".txt"
    elif simulationMode == 1:
        blockInfosLogFileName += "ethane_simulate_block_infos_" + str(startBlockNum) + "_" + str(endBlockNum) \
        + "_" + str(deleteEpoch) + "_" + str(inactivateEpoch) + "_" + str(inactivateCriterion) + ".txt"
    elif simulationMode == 2:
        blockInfosLogFileName += "ethanos_simulate_block_infos_" + str(startBlockNum) + "_" + str(endBlockNum) + "_" + str(inactivateEpoch) + ".txt"
    else:
        print("wrong mode:", simulationMode)
        sys.exit()

    # NodeStat.ToString() = totalNum, totalSize, fullNodeNum, fullNodeSize, shortNodeNum, shortNodeSize, leafNodeNum, leafNodeSize
    # logs[0~7][blockNum] = NewNodeStat.ToString()
    # logs[8~15][blockNum] = NewStorageNodeStat.ToString()
    # logs[16~23][blockNum] = TotalNodeStat.ToString()
    # logs[24~31][blockNum] = TotalStorageNodeStat.ToString()

    # logs[32][blockNum] = TimeToFlush (ns)
    # logs[33][blockNum] = TimeToDelete (ns)
    # logs[34][blockNum] = TimeToInactivate (ns)

    # logs[35][blockNum] = DeletedNodeNum
    # logs[36][blockNum] = RestoredNodeNum
    # logs[37][blockNum] = InactivatedNodeNum

    columnNum = 38
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

        for i in range(columnNum):
            logs[i][blockNum] = params[i]

        blockNum += 1
        # if blockNum > 100000:
        #     break
    
    f.close()

    #
    # draw graphs
    #
    
    # set x axis
    blockNums = list(range(startBlockNum,endBlockNum+1))
    blockNums = blockNums[:len(logs[0])]

    # set title
    if simulationMode == 0:
        graphTitlePrefix = "ethereum "
        graphTitleSuffix = " (epoch: " + str(inactivateEpoch) + ")"
    elif simulationMode == 1:
        graphTitlePrefix = "ethane "
        graphTitleSuffix = " (de: " + str(deleteEpoch) + ", ie: " + str(inactivateEpoch) + ", ic: " + str(inactivateCriterion) + ")"
    elif simulationMode == 2:
        graphTitlePrefix = "ethanos "
        graphTitleSuffix = " (epoch: " + str(inactivateEpoch) + ")"
    else:
        print("wrong mode:", simulationMode)
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



# draw graphs for block infos log file
def drawGraphsForTrieInspects(startBlockNum, endBlockNum, deleteEpoch=0, inactivateEpoch=0, inactivateCriterion=0):
    
    # get log file name
    trieInspectsLogFileName = ""
    if simulationMode == 0:
        trieInspectsLogFileName += "ethereum_simulate_trie_inspects_" + str(startBlockNum) + "_" + str(endBlockNum) + "_" + str(inactivateEpoch) + ".txt"
    elif simulationMode == 1:
        trieInspectsLogFileName += "ethane_simulate_trie_inspects_" + str(startBlockNum) + "_" + str(endBlockNum) \
        + "_" + str(deleteEpoch) + "_" + str(inactivateEpoch) + "_" + str(inactivateCriterion) + ".txt"
    elif simulationMode == 2:
        trieInspectsLogFileName += "ethanos_simulate_trie_inspects_" + str(startBlockNum) + "_" + str(endBlockNum) + "_" + str(inactivateEpoch) + ".txt"
    else:
        print("wrong mode:", simulationMode)
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
        if simulationMode == 1:
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
    if simulationMode == 0:
        graphTitlePrefix = "ethereum "
        graphTitleSuffix = " (epoch: " + str(inactivateEpoch) + ")"
    elif simulationMode == 1:
        graphTitlePrefix = "ethane "
        graphTitleSuffix = " (de: " + str(deleteEpoch) + ", ie: " + str(inactivateEpoch) + ", ic: " + str(inactivateCriterion) + ")"
    elif simulationMode == 2:
        graphTitlePrefix = "ethanos "
        graphTitleSuffix = " (epoch: " + str(inactivateEpoch) + ")"
    else:
        print("wrong mode:", simulationMode)
        sys.exit()

    # draw graph
    if simulationMode == 0:
        # draw graph: state trie node nums
        title = graphTitlePrefix + 'state trie node num' + graphTitleSuffix
        drawNodeStatGraphs(blockNums, logs[2][:len(blockNums)], logs[4][:len(blockNums)], logs[6][:len(blockNums)], 'block num', 'state trie node num', title)

        # draw graph: state trie node sizes
        title = graphTitlePrefix + 'state trie node size' + graphTitleSuffix
        drawNodeStatGraphs(blockNums, logs[3][:len(blockNums)], logs[5][:len(blockNums)], logs[7][:len(blockNums)], 'block num', 'state trie node size (B)', title)

    elif simulationMode == 1:
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

    elif simulationMode == 2:
        # draw graph: cached state trie node nums
        title = graphTitlePrefix + 'cached state trie node num' + graphTitleSuffix
        drawNodeStatGraphs(blockNums, logs[2][:len(blockNums)], logs[4][:len(blockNums)], logs[6][:len(blockNums)], 'block num', 'state trie node num', title)

        # draw graph: cached state trie node sizes
        title = graphTitlePrefix + 'cached state trie node size' + graphTitleSuffix
        drawNodeStatGraphs(blockNums, logs[3][:len(blockNums)], logs[5][:len(blockNums)], logs[7][:len(blockNums)], 'block num', 'state trie node size (B)', title)



if __name__ == "__main__":
    print("start")

    # set simulation mode
    simulationMode = 0
    # set simulation params
    startBlockNum = 0
    endBlockNum = 100000
    deleteEpoch = 10000
    inactivateEpoch = 10000
    inactivateCriterion = 10000

    # draw graphs
    # drawGraphsForBlockInfos(startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)
    # drawGraphsForTrieInspects(startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)

    # draw graphs for all modes
    for i in range(3):
        simulationMode = i
        drawGraphsForBlockInfos(startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)
        drawGraphsForTrieInspects(startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)

    print("end")
