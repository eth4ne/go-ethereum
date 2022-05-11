
cd ../..
make geth
cd build/bin


DataDir="/ssd/ethereum"




./geth --datadir ${DataDir} db trieinspect $1

