import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
import numpy as np

t1 = np.loadtxt("../Trie10K.txt",delimiter=" ",unpack = False)
t2 = np.loadtxt("../Trie100K.txt",delimiter=" ",unpack = False)
t3 = np.loadtxt("../Trie1M.txt",delimiter=" ",unpack = False)

plt.title('Trie getBalance Query time')

plt.plot(t3, label='1M Accounts', linewidth=1)
plt.plot(t2, label='100K Accounts', linewidth=1)
plt.plot(t1, label='10K Accounts', linewidth=1)


plt.ylim([0, 6000000])
plt.margins(0.05, 0.7)


# axis labeling
plt.xlabel('Account num in log scale')
plt.ylabel('Nano Second')

# x-axis: log scale
# plt.xscale('symlog') 

# legend
plt.legend()

# export
# plt.savefig('result_1/10K-100K-1M-Trie.png')
plt.savefig('result_1/10K-100K-1M-Trie_yLimit.png')