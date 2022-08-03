cd ../..
make geth
cd build/bin


# DataDir="/ssd/ethereum"
DataDir="/ssd/ethereum-vanilla"
echo "clear chaindata"
rm -rf ${DataDir} && mkdir ${DataDir}
./geth --datadir ${DataDir} --syncmode=full --gcmode=archive --http --http.port "8081" --http.corsdomain="*" --http.api="admin,eth,debug,miner,net,txpool,personal,web3" --snapshot=false

