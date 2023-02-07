# #!/bin/bash
# export path=/ethereum/hletrd/data
# #read -p "Geth data dir: " path
# make -C ../ geth
# rm -rf $path/geth
# ./bin/geth --datadir $path init frontier.json

#!/bin/bash
cd ../ && make geth
cd build
export path=/ethereum/joonha/data
#read -p "Geth data dir: " path
rm -rf $path/geth
./bin/geth --datadir $path init frontier.json