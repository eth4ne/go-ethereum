cd ../..
make geth
rm -rvf txDetail/*

DataDir="/ssd/ethereum"
rm -rf ${DataDir} && mkdir ${DataDir}

cd build/bin
./geth --datadir ${DataDir} --syncmode=full --gcmode=archive --http --http.port "8081" --http.corsdomain="*" --http.api="admin,eth,debug,miner,net,txpool,personal,web3" --snapshot=false 

