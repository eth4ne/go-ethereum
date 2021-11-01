import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
import numpy as np

s1 = np.loadtxt("../Snap10K.txt",delimiter=" ",unpack = False)
t1 = np.loadtxt("../Trie10K.txt",delimiter=" ",unpack = False)

plt.title('unify AccountRLP no cache 10000/200')

plt.plot(s1, label='Snapshot', linewidth=1)
plt.plot(t1, label='Trie', linewidth=1)

plt.xlabel('Account Num')
plt.ylabel('Nano Second')

plt.legend()

plt.savefig('result_1/SnapVsTrie(10000-200)_1.png')