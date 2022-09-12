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
isEthane = True

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
    ax.xaxis.set_major_locator(MaxNLocator(5)) # set # of ticks
    ax.yaxis.set_major_locator(MaxNLocator(5)) # set # of ticks

    # save graph
    makeDir(graphPath)
    graphName = graphTitle + ".png"
    plt.savefig(graphPath+graphName, format="png")



# draw graphs for block infos log file
def drawGraphsForBlockInfos(startBlockNum, endBlockNum, deleteEpoch=0, inactivateEpoch=0, inactivateCriterion=0):
    
    # get log file name
    blockInfosLogFileName = ""
    if isEthane:
        blockInfosLogFileName += "ethane_simulate_block_infos_" + str(startBlockNum) + "_" + str(endBlockNum) \
        + "_" + str(deleteEpoch) + "_" + str(inactivateEpoch) + "_" + str(inactivateCriterion) + ".txt"
    else:
        blockInfosLogFileName += "ethereum_simulate_block_infos_" + str(startBlockNum) + "_" + str(endBlockNum) + ".txt"

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
    graphTitlePrefix = "ethereum "
    if isEthane:
        graphTitlePrefix = "ethane "
    
    # draw graph: NewNodeStat -> nums
    # title = 'new state trie node num'
    # drawNodeStatGraphs(blockNums, logs[2], logs[4], logs[6], 'block num', 'state trie node num', graphTitlePrefix+title)

    # draw graph: NewNodeStat -> sizes
    # title = 'new state trie node size'
    # drawNodeStatGraphs(blockNums, logs[3], logs[5], logs[7], 'block num', 'state trie node size (B)', graphTitlePrefix+title)

    # draw graph: TotalNodeStat -> nums
    title = 'archive state trie node num'
    drawNodeStatGraphs(blockNums, logs[18], logs[20], logs[22], 'block num', 'state trie node num', graphTitlePrefix+title)

    # draw graph: TotalNodeStat -> sizes
    title = 'archive state trie node size'
    drawNodeStatGraphs(blockNums, logs[19], logs[21], logs[23], 'block num', 'state trie node size (B)', graphTitlePrefix+title)



if __name__ == "__main__":
    print("start")

    # set options
    isEthane = False
    # draw graph
    drawGraphsForBlockInfos(0, 100000, 315, 315, 315)

    # isEthane = True
    # drawGraphsForBlockInfos(0, 100000)

    print("end")
