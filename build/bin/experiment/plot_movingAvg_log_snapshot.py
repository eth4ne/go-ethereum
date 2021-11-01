from __future__ import division
import matplotlib
matplotlib.use('Agg')
from pylab import plot, ylim, xlim, show, xlabel, ylabel, grid
from numpy import linspace, loadtxt, ones, convolve
import numpy as numpy
import matplotlib.pyplot as plt


data1 = loadtxt("../Snap10K.txt", float)
data2 = loadtxt("../Snap100K.txt", float)
data3 = loadtxt("../Snap1M.txt", float)

def movingaverage(interval, window_size):
    window= numpy.ones(int(window_size))/float(window_size)
    return numpy.convolve(interval, window, 'same')

y1 = data1[:]
y2 = data2[:]
y3 = data3[:]

# plt.plot(y1)
# plt.plot(y2)
# plt.plot(y3)

y_av1 = movingaverage(y1, 40)
y_av2 = movingaverage(y2, 40)
y_av3 = movingaverage(y3, 40)

# axis labeling
plt.xlabel('Account num in log scale')
plt.ylabel('Nano Second')

plt.plot(y_av1, label='10K Accounts')
plt.plot(y_av2, label='100K Accounts')
plt.plot(y_av3, label='1M Accounts')

plt.ticklabel_format(axis="y", style="sci", scilimits=(0,0))

plt.title('Snapshot getBalance Query time (MovAvg:40)')

plt.legend()

# log scale
plt.xscale('symlog') 

plt.savefig('result_1/10K-100K-1M-Snap_movAvg_log.png')