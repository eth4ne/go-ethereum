import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
import numpy as np

s1 = np.loadtxt("../Snap10K.txt",delimiter=" ",unpack = False)
s2 = np.loadtxt("../Snap100K.txt",delimiter=" ",unpack = False)
s3 = np.loadtxt("../Snap1M.txt",delimiter=" ",unpack = False)

plt.title('Snapshot getBalance Query time')

# plt.plot(s3, label='1M Accounts', linewidth=1, color='dimgrey')
# plt.plot(s2, label='100K Accounts', linewidth=1, color='silver')
# plt.plot(s1, label='10K Accounts', linewidth=1, color='black')

plt.plot(s3, label='1M Accounts', linewidth=1)
plt.plot(s2, label='100K Accounts', linewidth=1)
plt.plot(s1, label='10K Accounts', linewidth=1)

# axis labeling
plt.xlabel('Account num in log scale')
plt.ylabel('Nano Second')

# x-axis: log scale
# plt.xscale('symlog') 

# legend
plt.legend()

# export
# plt.savefig('result_1/10K-100K-1M-Snap.png')
plt.savefig('result_1/10K-100K-1M-Snap_yLimit.png')