// SPDX-License-Identifier: MIT
pragma solidity ^0.5.1;

// byzantium

contract SimpleStorage {
    
    uint256 a;     // slot 0
    uint256[2] b;  // slots 1-2
    mapping (uint256 => uint256) public storageMap; // slot 3
    
    function set(uint key, uint value) public {
        a = key;
        b[1] = value;
        // storageMap[key] = value;
    }

    function get(uint key) public view returns (uint) {
        return storageMap[key];
    }
    
    function mapLocation(uint256 slot, uint256 key) public pure returns (uint256) {
        return uint256(keccak256(abi.encodePacked(key, slot)));
    }

    function () external payable {}
}