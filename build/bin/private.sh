## Connect to remix.ethereum.org via Web3 provider
# Jhkim: You should go http://remix.ethereum.org not https


# remove exists txDetail dir
cd ../..
rm -rvf txDetail/*
make geth
cd build/bin


# WARNING: delete geth datadir. 
echo "clear chaindata"

# DataDir="/ssd/ethereum"
# DataDir="/home/jhkim/go/src/github.com/ethereum/go-ethereum/build/bin/experiment/experiment"
DataDir="./chaindata"
rm -vrf ${DataDir} && mkdir ${DataDir}
cp -r ./keystore ${DataDir}/keystore


# init geth
echo "geth init"
./geth init genesis.json  --datadir ${DataDir}

# run geth
echo "geth run"


if [ -n "$1" ]; then # if arguements exists
	echo 'epoch of txdetail:' $1
	./geth --datadir ${DataDir} --keystore "./keystore" --allow-insecure-unlock --networkid 12345 \
	--syncmode=full --gcmode=archive \
	--http --http.addr "0.0.0.0" --http.port "8081" --port 30303 --http.corsdomain "*" --http.api="admin,eth,debug,miner,net,txpool,personal,web3" --http.vhosts "lynx.snu.ac.kr"\
	--snapshot=false --txdetail $1 --nodiscover --maxpeers 0

else
	echo 'default epoch of txdetail: 100000'
	./geth --datadir ${DataDir} --keystore "./keystore" --allow-insecure-unlock --networkid 12345 \
	--syncmode=full --gcmode=archive \
	--http --http.addr "0.0.0.0" --http.port "8081" --port 30303 --http.corsdomain "*" --http.api="admin,eth,debug,miner,net,txpool,personal,web3" --http.vhosts "lynx.snu.ac.kr"\
	--snapshot=false --txdetail 100000 --nodiscover --maxpeers 0

fi