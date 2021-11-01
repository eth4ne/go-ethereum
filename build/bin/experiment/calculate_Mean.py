# Snapshot avg

print("=====SNAPSHOT=====")

##################

f = open("../Snap10K.txt", 'r')
score = f.readlines() 
score = list(map(int, score)) 
score_sum = 0

for i in score: 
	score_sum = score_sum + i
score_average = score_sum / len(score)

print("10K sum: %d"%score_sum)
print("10K avg: %d"%score_average)

###################

f2 = open("../Snap100K.txt", 'r')
score2 = f2.readlines() 
score2 = list(map(int, score2)) 
score_sum2 = 0

for i in score2: 
	score_sum2 = score_sum2 + i
score_average2 = score_sum2 / len(score2)

print("100K sum: %d"%score_sum2)
print("100K avg: %d"%score_average2)

###################

f3 = open("../Snap1M.txt", 'r')
score3 = f3.readlines() 
score3 = list(map(int, score3)) 
score_sum3 = 0

for i in score3: 
	score_sum3 = score_sum3 + i
score_average3 = score_sum3 / len(score3)

print("1M sum: %d"%score_sum3)
print("1M avg: %d"%score_average3)


######
######

# Trie avg

print("=====TRIE=====")

##################

f4 = open("../Trie10K.txt", 'r')
score4 = f4.readlines() 
score4 = list(map(int, score4)) 
score_sum4 = 0

for i in score4: 
	score_sum4 = score_sum4 + i
score_average4 = score_sum4 / len(score4)

print("10K sum: %d"%score_sum4)
print("10K avg: %d"%score_average4)

###################

f5 = open("../Trie100K.txt", 'r')
score5 = f5.readlines() 
score5 = list(map(int, score5)) 
score_sum5 = 0

for i in score5: 
	score_sum5 = score_sum5 + i
score_average5 = score_sum5 / len(score5)

print("100K sum: %d"%score_sum5)
print("100K avg: %d"%score_average5)

###################

f6 = open("../Trie1M.txt", 'r')
score6 = f6.readlines() 
score6 = list(map(int, score6)) 
score_sum6 = 0

for i in score6: 
	score_sum6 = score_sum6 + i
score_average6 = score_sum6 / len(score6)

print("1M sum: %d"%score_sum6)
print("1M avg: %d"%score_average6)
