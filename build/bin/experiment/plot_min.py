import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
import numpy as np

sm = np.loadtxt("../SnapMin.txt",delimiter=" ",unpack = False)

tm = np.loadtxt("../TrieMin.txt",delimiter=" ",unpack = False)

plt.title('AccountRLP no cache min 10000/1')

plt.plot(sm, label='Snapshot Min')
plt.plot(tm, label='Trie Min')

plt.xlabel('Account Num')
plt.ylabel('Nano Second')

plt.legend()

plt.savefig('result/SnapVsTrie(10000-1)min_RLP.png')