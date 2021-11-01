import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
import numpy as np

sm = np.loadtxt("../SnapMEAN.txt",delimiter=" ",unpack = False)
smd = np.loadtxt("../SnapMEDIAN.txt",delimiter=" ",unpack = False)

tm = np.loadtxt("../TrieMEAN.txt",delimiter=" ",unpack = False)
tmd = np.loadtxt("../TrieMEDIAN.txt",delimiter=" ",unpack = False)

plt.title('AccountRLP no cache 10000/1')

plt.plot(sm, label='Snapshot Mean')
plt.plot(smd, label='Snapshot Median')
plt.plot(tm, label='Trie Mean')
plt.plot(tmd, label='Trie Medain')

plt.xlabel('Account Num')
plt.ylabel('Nano Second')

plt.legend()

plt.savefig('result/SnapVsTrie(10000-1)MM_RLP.png')