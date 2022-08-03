
cd ../..
make geth
cd build/bin


#DataDir="/ssd/ethereum"
DataDir="/ssd/ethereum-vanilla"



./geth --datadir ${DataDir} db trieinspect $1

