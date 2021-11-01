import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
import numpy as np

# s = np.loadtxt("../backup_result/100000-1 no cache/Snap1.txt",delimiter=" ",unpack = False)
t = np.loadtxt("../backup_result/100000-1 no cache/Trie1.txt",delimiter=" ",unpack = False)


plt.title('no cache 100000/1')

# plt.plot(s, label='Snapshot')
plt.plot(t, label='Trie')

plt.xlabel('Account Num')
plt.ylabel('Nano Second')

plt.legend()

plt.savefig('result/SnapVsTrie(100000-1)_1_Trie.png')