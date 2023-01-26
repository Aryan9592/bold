// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE
// SPDX-License-Identifier: BUSL-1.1

pragma solidity ^0.8.0;

import "@openzeppelin/contracts-upgradeable/security/PausableUpgradeable.sol";

import "./Node.sol";
import "./IRollupCore.sol";
import "./RollupLib.sol";
import "./AssertionLib.sol";
import "./IRollupEventInbox.sol";
import "./IRollupCore.sol";

import "../challenge/IChallengeManager.sol";

import "../bridge/ISequencerInbox.sol";
import "../bridge/IBridge.sol";
import "../bridge/IOutbox.sol";

import {NO_CHAL_INDEX} from "../libraries/Constants.sol";

abstract contract RollupCore is IRollupCore, PausableUpgradeable {
    using NodeLib for Node;
    using GlobalStateLib for GlobalState;
    using AssertionLib for Assertion;

    // Rollup Config
    uint64 public confirmPeriodBlocks;
    uint64 public extraChallengeTimeBlocks;
    uint256 public chainId;
    uint256 public baseStake;
    bytes32 public wasmModuleRoot;

    IInbox public inbox;
    IBridge public bridge;
    IOutbox public outbox;
    ISequencerInbox public sequencerInbox;
    IRollupEventInbox public rollupEventInbox;
    IChallengeManager public override challengeManager;

    // misc useful contracts when interacting with the rollup
    address public validatorUtils;
    address public validatorWalletCreator;

    // when a staker loses a challenge, half of their funds get escrowed in this address
    address public loserStakeEscrow;
    address public stakeToken;
    uint256 public minimumAssertionPeriod;

    mapping(address => bool) public isValidator;

    // Stakers become Zombies after losing a challenge
    struct Zombie {
        address stakerAddress;
        uint64 latestStakedNode;
    }

    uint64 private _latestConfirmed;
    uint64 private _firstUnresolvedNode;
    uint64 private _latestNodeCreated;
    uint64 private _lastStakeBlock;
    mapping(uint64 => Node) private _nodes;
    mapping(uint64 => mapping(address => bool)) private _nodeStakers;

    address[] private _stakerList;
    mapping(address => Staker) public _stakerMap;

    Zombie[] private _zombies;

    mapping(address => uint256) private _withdrawableFunds;
    uint256 public totalWithdrawableFunds;
    uint256 public rollupDeploymentBlock;

    // The node number of the initial node
    uint64 internal constant GENESIS_NODE = 0;

    bool public validatorWhitelistDisabled;

    // Add new storage variables for new rollup below
    mapping(uint64 => Assertion) private _assertions;
    mapping(bytes32 => uint64) private _assertionToNum;
    uint64 private _firstUnresolvedAssertion;
    uint64 private _latestAssertionCreated;
    uint64 private _latestConfirmedAssertion;

    /**
     * @notice Get a storage reference to the Node for the given node index
     * @param nodeNum Index of the node
     * @return Node struct
     */
    function getNodeStorage(uint64 nodeNum) internal view returns (Node storage) {
        revert("DEPRECATED getNodeStorage");
    }

    /**
     * @notice Get the Node for the given index.
     */
    function getNode(uint64 nodeNum) public view override returns (Node memory) {
        revert("DEPRECATED getNode");
    }


    /**
     * @notice Get a storage reference to the Assertion for the given index
     * @param seqNum Index of the Assertion
     * @return Assertion struct
     */
    function getAssertionStorage(uint64 seqNum) internal view returns (Assertion storage) {
        return _assertions[seqNum];
    }

    /**
     * @notice Get the Assertion for the given index.
     */
    function getAssertion(uint64 seqNum) public view returns (Assertion memory) {
        return getAssertionStorage(seqNum);
    }

    /**
     * @notice Check if the specified node has been staked on by the provided staker.
     * Only accurate at the latest confirmed node and afterwards.
     */
    function nodeHasStaker(uint64 nodeNum, address staker) public view override returns (bool) {
        return _nodeStakers[nodeNum][staker];
    }

    /**
     * @notice Get the address of the staker at the given index
     * @param stakerNum Index of the staker
     * @return Address of the staker
     */
    function getStakerAddress(uint64 stakerNum) external view override returns (address) {
        return _stakerList[stakerNum];
    }

    /**
     * @notice Check whether the given staker is staked
     * @param staker Staker address to check
     * @return True or False for whether the staker was staked
     */
    function isStaked(address staker) public view override returns (bool) {
        return _stakerMap[staker].isStaked;
    }

    /**
     * @notice Check whether the given staker is staked on the latest confirmed node,
     * which includes if the staker is staked on a descendent of the latest confirmed node.
     * @param staker Staker address to check
     * @return True or False for whether the staker was staked
     */
    function isStakedOnLatestConfirmed(address staker) public view returns (bool) {
        revert("DEPRECATED isStakedOnLatestConfirmed");
    }

    /**
     * @notice Get the latest staked node of the given staker
     * @param staker Staker address to lookup
     * @return Latest node staked of the staker
     */
    function latestStakedNode(address staker) public view override returns (uint64) {
        revert("DEPRECATED latestStakedNode");
    }

    /**
     * @notice Get the latest staked assertion of the given staker
     * @param staker Staker address to lookup
     * @return Latest assertion staked of the staker
     */
    function latestStakedAssertion(address staker) public view returns (uint64) {
        return _stakerMap[staker].latestStakedAssertion;
    }

    /**
     * @notice Get the current challenge of the given staker
     * @param staker Staker address to lookup
     * @return Current challenge of the staker
     */
    function currentChallenge(address staker) public view override returns (uint64) {
        revert("DEPRECATED currentChallenge");
    }

    /**
     * @notice Get the amount staked of the given staker
     * @param staker Staker address to lookup
     * @return Amount staked of the staker
     */
    function amountStaked(address staker) public view override returns (uint256) {
        return _stakerMap[staker].amountStaked;
    }

    /**
     * @notice Retrieves stored information about a requested staker
     * @param staker Staker address to retrieve
     * @return A structure with information about the requested staker
     */
    function getStaker(address staker) external view override returns (Staker memory) {
        return _stakerMap[staker];
    }

    /**
     * @notice Get the original staker address of the zombie at the given index
     * @param zombieNum Index of the zombie to lookup
     * @return Original staker address of the zombie
     */
    function zombieAddress(uint256 zombieNum) public view override returns (address) {
        return _zombies[zombieNum].stakerAddress;
    }

    /**
     * @notice Get Latest node that the given zombie at the given index is staked on
     * @param zombieNum Index of the zombie to lookup
     * @return Latest node that the given zombie is staked on
     */
    function zombieLatestStakedNode(uint256 zombieNum) public view override returns (uint64) {
        revert("DEPRECATED zombieLatestStakedNode");
    }

    /**
     * @notice Retrieves stored information about a requested zombie
     * @param zombieNum Index of the zombie to lookup
     * @return A structure with information about the requested staker
     */
    function getZombieStorage(uint256 zombieNum) internal view returns (Zombie storage) {
        return _zombies[zombieNum];
    }

    /// @return Current number of un-removed zombies
    function zombieCount() public view override returns (uint256) {
        return _zombies.length;
    }

    function isZombie(address staker) public view override returns (bool) {
        for (uint256 i = 0; i < _zombies.length; i++) {
            if (staker == _zombies[i].stakerAddress) {
                return true;
            }
        }
        return false;
    }

    /**
     * @notice Get the amount of funds withdrawable by the given address
     * @param user Address to check the funds of
     * @return Amount of funds withdrawable by user
     */
    function withdrawableFunds(address user) external view override returns (uint256) {
        return _withdrawableFunds[user];
    }

    /**
     * @return Index of the first unresolved node
     * @dev If all nodes have been resolved, this will be latestNodeCreated + 1
     */
    function firstUnresolvedNode() public view override returns (uint64) {
        return _firstUnresolvedNode;
    }

    /**
     * @return Index of the first unresolved node
     * @dev If all nodes have been resolved, this will be latestNodeCreated + 1
     */
    function firstUnresolvedAssertion() public view returns (uint64) {
        return _firstUnresolvedAssertion;
    }

    /// @return Index of the latest confirmed node
    function latestConfirmed() public view override returns (uint64) {
        return _latestConfirmed;
    }

    /// @return Index of the latest confirmed assertion
    function latestConfirmedAssertion() public view returns (uint64) {
        return _latestConfirmedAssertion;
    }

    /// @return Index of the latest rollup node created
    function latestNodeCreated() public view override returns (uint64) {
        revert("DEPRECATED latestNodeCreated");
    }

    /// @return Index of the latest rollup node created
    function latestAssertionCreated() public view returns (uint64) {
        return _latestAssertionCreated;
    }

    /// @return Ethereum block that the most recent stake was created
    function lastStakeBlock() external view override returns (uint64) {
        return _lastStakeBlock;
    }

    /// @return Number of active stakers currently staked
    function stakerCount() public view override returns (uint64) {
        return uint64(_stakerList.length);
    }

    /**
     * @notice Initialize the core with an initial assertion
     * @param initialAssertion Initial assertion to start the chain with
     */
    function initializeCore(Assertion memory initialAssertion) internal {
        __Pausable_init();
        _assertions[GENESIS_NODE] = initialAssertion;
        _latestAssertionCreated = GENESIS_NODE;
        _firstUnresolvedAssertion = GENESIS_NODE + 1;
    }

    /**
     * @notice React to a new node being created by storing it an incrementing the latest node counter
     * @param node Node that was newly created
     */
    function nodeCreated(Node memory node) internal {
        revert("DEPRECATED nodeCreated");
    }

    /**
     * @notice React to a new assertion being created by storing it an incrementing the latest assertion counter
     * @param assertion Assertion that was newly created
     */
    function assertionCreated(Assertion memory assertion) internal {
        // TODO: can we not copy this assertion to memory?
        _latestAssertionCreated++;
        _assertions[_latestAssertionCreated] = assertion; 
    }

    /// @notice Reject the next unresolved node
    function _rejectNextNode() internal {
        revert("DEPRECATED _rejectNextNode");
        _firstUnresolvedNode++;
    }

    /// @notice Reject the next unresolved node
    function _rejectNextAssertion() internal {
        _firstUnresolvedAssertion++;
    }

    function confirmNode(
        uint64 nodeNum,
        bytes32 blockHash,
        bytes32 sendRoot
    ) internal {
        revert("DEPRECATED");
        Node storage node = getNodeStorage(nodeNum);
        // Authenticate data against node's confirm data pre-image
        require(node.confirmData == RollupLib.confirmHash(blockHash, sendRoot), "CONFIRM_DATA");

        // trusted external call to outbox
        outbox.updateSendRoot(sendRoot, blockHash);

        _latestConfirmed = nodeNum;
        _firstUnresolvedNode = nodeNum + 1;

        emit NodeConfirmed(nodeNum, blockHash, sendRoot);
    }

    function confirmAssertion(
        uint64 assertionNum,
        bytes32 blockHash,
        bytes32 sendRoot
    ) internal {
        Assertion storage assertion = getAssertionStorage(assertionNum);
        // Authenticate data against node's confirm data pre-image
        require(assertion.confirmHash == RollupLib.confirmHash(blockHash, sendRoot), "CONFIRM_DATA");

        // trusted external call to outbox
        outbox.updateSendRoot(sendRoot, blockHash);

        _latestConfirmedAssertion = assertionNum;
        _firstUnresolvedAssertion = assertionNum + 1;

        emit NodeConfirmed(assertionNum, blockHash, sendRoot); // TODO: fix event
    }

    /**
     * @notice Create a new stake at latest confirmed node
     * @param stakerAddress Address of the new staker
     * @param depositAmount Stake amount of the new staker
     */
    function createNewStake(address stakerAddress, uint256 depositAmount) internal {
        uint64 stakerIndex = uint64(_stakerList.length);
        _stakerList.push(stakerAddress);
        _stakerMap[stakerAddress] = Staker(
            depositAmount,
            stakerIndex,
            0, // TODO: 
            NO_CHAL_INDEX, // new staker is not in challenge
            false
        );
        // _nodeStakers[_latestConfirmedAssertion][stakerAddress] = true;
        _lastStakeBlock = uint64(block.number);
        emit UserStakeUpdated(stakerAddress, 0, depositAmount);
    }

    /**
     * @notice Check to see whether the two stakers are in the same challenge
     * @param stakerAddress1 Address of the first staker
     * @param stakerAddress2 Address of the second staker
     * @return Address of the challenge that the two stakers are in
     */
    function inChallenge(address stakerAddress1, address stakerAddress2)
        internal
        view
        returns (uint64)
    {
        revert("DEPRECATED inChallenge");
    }

    /**
     * @notice Make the given staker as not being in a challenge
     * @param stakerAddress Address of the staker to remove from a challenge
     */
    function clearChallenge(address stakerAddress) internal {
        revert("DEPRECATED clearChallenge");
    }

    /**
     * @notice Mark both the given stakers as engaged in the challenge
     * @param staker1 Address of the first staker
     * @param staker2 Address of the second staker
     * @param challenge Address of the challenge both stakers are now in
     */
    function challengeStarted(
        address staker1,
        address staker2,
        uint64 challenge
    ) internal {
        revert("DEPRECATED challengeStarted");
    }

    /**
     * @notice Add to the stake of the given staker by the given amount
     * @param stakerAddress Address of the staker to increase the stake of
     * @param amountAdded Amount of stake to add to the staker
     */
    function increaseStakeBy(address stakerAddress, uint256 amountAdded) internal {
        Staker storage staker = _stakerMap[stakerAddress];
        uint256 initialStaked = staker.amountStaked;
        uint256 finalStaked = initialStaked + amountAdded;
        staker.amountStaked = finalStaked;
        emit UserStakeUpdated(stakerAddress, initialStaked, finalStaked);
    }

    /**
     * @notice Reduce the stake of the given staker to the given target
     * @param stakerAddress Address of the staker to reduce the stake of
     * @param target Amount of stake to leave with the staker
     * @return Amount of value released from the stake
     */
    function reduceStakeTo(address stakerAddress, uint256 target) internal returns (uint256) {
        Staker storage staker = _stakerMap[stakerAddress];
        uint256 current = staker.amountStaked;
        require(target <= current, "TOO_LITTLE_STAKE");
        uint256 amountWithdrawn = current - target;
        staker.amountStaked = target;
        increaseWithdrawableFunds(stakerAddress, amountWithdrawn);
        emit UserStakeUpdated(stakerAddress, current, target);
        return amountWithdrawn;
    }

    /**
     * @notice Remove the given staker and turn them into a zombie
     * @param stakerAddress Address of the staker to remove
     */
    function turnIntoZombie(address stakerAddress) internal {
        revert("DEPRECATED turnIntoZombie");
    }

    /**
     * @notice Update the latest staked node of the zombie at the given index
     * @param zombieNum Index of the zombie to move
     * @param latest New latest node the zombie is staked on
     */
    function zombieUpdateLatestStakedNode(uint256 zombieNum, uint64 latest) internal {
        revert("DEPRECATED zombieUpdateLatestStakedNode");
    }

    /**
     * @notice Remove the zombie at the given index
     * @param zombieNum Index of the zombie to remove
     */
    function removeZombie(uint256 zombieNum) internal {
        _zombies[zombieNum] = _zombies[_zombies.length - 1];
        _zombies.pop();
    }

    /**
     * @notice Mark the given staker as staked on this node
     * @param staker Address of the staker to mark
     */
    function addStaker(uint64 nodeNum, address staker) internal {
        require(!_nodeStakers[nodeNum][staker], "ALREADY_STAKED");
        _nodeStakers[nodeNum][staker] = true;
        Node storage node = getNodeStorage(nodeNum);
        require(node.deadlineBlock != 0, "NO_NODE");

        uint64 prevCount = node.stakerCount;
        node.stakerCount = prevCount + 1;

        if (nodeNum > GENESIS_NODE) {
            Node storage parent = getNodeStorage(node.prevNum);
            parent.childStakerCount++;
            if (prevCount == 0) {
                parent.newChildConfirmDeadline(uint64(block.number) + confirmPeriodBlocks);
            }
        }
    }

    /**
     * @notice Remove the given staker from this node
     * @param staker Address of the staker to remove
     */
    function removeStaker(uint64 nodeNum, address staker) internal {
        require(_nodeStakers[nodeNum][staker], "NOT_STAKED");
        _nodeStakers[nodeNum][staker] = false;

        Node storage node = getNodeStorage(nodeNum);
        node.stakerCount--;

        if (nodeNum > GENESIS_NODE) {
            getNodeStorage(node.prevNum).childStakerCount--;
        }
    }

    /**
     * @notice Remove the given staker and return their stake
     * This should not be called if the staker is staked on a descendent of the latest confirmed node
     * @param stakerAddress Address of the staker withdrawing their stake
     */
    function withdrawStaker(address stakerAddress) internal {
        revert("NOT_IMPLEMENTED");
        Staker storage staker = _stakerMap[stakerAddress];
        uint64 latestConfirmedNum = latestConfirmedAssertion();
        if (nodeHasStaker(latestConfirmedNum, stakerAddress)) {
            // Withdrawing a staker whose latest staked node isn't resolved should be impossible
            assert(staker.latestStakedAssertion == latestConfirmedNum);
            removeStaker(latestConfirmedNum, stakerAddress);
        }
        uint256 initialStaked = staker.amountStaked;
        increaseWithdrawableFunds(stakerAddress, initialStaked);
        deleteStaker(stakerAddress);
        emit UserStakeUpdated(stakerAddress, initialStaked, 0);
    }

    /**
     * @notice Advance the given staker to the given node
     * @param stakerAddress Address of the staker adding their stake
     * @param nodeNum Index of the node to stake on
     */
    function stakeOnNode(address stakerAddress, uint64 nodeNum) internal {
        revert("NOT_IMPLEMENTED");
        Staker storage staker = _stakerMap[stakerAddress];
        addStaker(nodeNum, stakerAddress);
        staker.latestStakedAssertion = nodeNum;
    }

    /**
     * @notice Advance the given staker to the given assertion
     * @param stakerAddress Address of the staker adding their stake
     * @param assertionNum Index of the assertion to stake on
     */
    function stakeOnAssertion(address stakerAddress, uint64 assertionNum) internal {
        Staker storage staker = _stakerMap[stakerAddress];
        getAssertionStorage(assertionNum).staker = stakerAddress;
        staker.latestStakedAssertion = assertionNum;
        staker.isStaked = true;
    }

    /**
     * @notice Clear the withdrawable funds for the given address
     * @param account Address of the account to remove funds from
     * @return Amount of funds removed from account
     */
    function withdrawFunds(address account) internal returns (uint256) {
        uint256 amount = _withdrawableFunds[account];
        _withdrawableFunds[account] = 0;
        totalWithdrawableFunds -= amount;
        emit UserWithdrawableFundsUpdated(account, amount, 0);
        return amount;
    }

    /**
     * @notice Increase the withdrawable funds for the given address
     * @param account Address of the account to add withdrawable funds to
     */
    function increaseWithdrawableFunds(address account, uint256 amount) internal {
        uint256 initialWithdrawable = _withdrawableFunds[account];
        uint256 finalWithdrawable = initialWithdrawable + amount;
        _withdrawableFunds[account] = finalWithdrawable;
        totalWithdrawableFunds += amount;
        emit UserWithdrawableFundsUpdated(account, initialWithdrawable, finalWithdrawable);
    }

    /**
     * @notice Remove the given staker
     * @param stakerAddress Address of the staker to remove
     */
    function deleteStaker(address stakerAddress) private {
        Staker storage staker = _stakerMap[stakerAddress];
        require(staker.isStaked, "NOT_STAKED");
        uint64 stakerIndex = staker.index;
        _stakerList[stakerIndex] = _stakerList[_stakerList.length - 1];
        _stakerMap[_stakerList[stakerIndex]].index = stakerIndex;
        _stakerList.pop();
        delete _stakerMap[stakerAddress];
    }

    struct StakeOnNewNodeFrame {
        uint256 currentInboxSize;
        Node node;
        bytes32 executionHash;
        Node prevNode;
        bytes32 lastHash;
        bool hasSibling;
        uint64 deadlineBlock;
        bytes32 sequencerBatchAcc;
    }

    struct StakeOnNewAssertionFrame {
        uint256 currentInboxSize;
        Assertion assertion;
        bytes32 executionHash;
        Assertion prevAssertion;
        bytes32 lastHash;
        bool notFirstChild;
        uint64 deadlineBlock;
        bytes32 sequencerBatchAcc;
    }

    function createNewNode(
        OldAssertion calldata assertion,
        uint64 prevNodeNum,
        uint256 prevNodeInboxMaxCount,
        bytes32 expectedNodeHash
    ) internal returns (bytes32 newNodeHash) {
        revert("DEPRECATED createNewNode");
    }

    function createNewAssertion(
        NewAssertionInputs calldata inputs
    ) internal returns (bytes32 newAssertionHash) {
        require(
            inputs.afterState.machineStatus == MachineStatus.FINISHED ||
                inputs.afterState.machineStatus == MachineStatus.ERRORED,
            "BAD_AFTER_STATUS"
        );

        StakeOnNewAssertionFrame memory memoryFrame;
        {
            // validate data
            memoryFrame.prevAssertion = getAssertion(inputs.prevNum);
            memoryFrame.currentInboxSize = bridge.sequencerMessageCount();

            // Make sure the previous state is correct against the node being built on
            require(
                RollupLib.stateHash(inputs.beforeState, inputs.prevNodeInboxMaxCount) ==
                    memoryFrame.prevAssertion.stateHash,
                "PREV_STATE_HASH"
            );

            // TODO: check what we need to check here since we already
            //       checked it consumed the exact amount of message in stakeOnNewAssertion
            // // Ensure that the assertion doesn't read past the end of the current inbox
            uint64 afterInboxCount = inputs.afterState.globalState.getInboxPosition();
            // uint64 prevInboxPosition = assertion.beforeState.globalState.getInboxPosition();
            // require(afterInboxCount >= prevInboxPosition, "INBOX_BACKWARDS");
            // if (afterInboxCount == prevInboxPosition) {
            //     require(
            //         assertion.afterState.globalState.getPositionInMessage() >=
            //             assertion.beforeState.globalState.getPositionInMessage(),
            //         "INBOX_POS_IN_MSG_BACKWARDS"
            //     );
            // }
            // See validator/assertion.go ExecutionState RequiredBatches() for reasoning
            if (
                inputs.afterState.machineStatus == MachineStatus.ERRORED ||
                inputs.afterState.globalState.getPositionInMessage() > 0
            ) {
                // The current inbox message was read
                afterInboxCount++;
            }
            require(afterInboxCount <= memoryFrame.currentInboxSize, "INBOX_PAST_END");
            // This gives replay protection against the state of the inbox
            if (afterInboxCount > 0) {
                memoryFrame.sequencerBatchAcc = bridge.sequencerInboxAccs(afterInboxCount - 1);
            }
        }

        {
            // TODO: figure out how to hash the assertion
            // memoryFrame.executionHash = RollupLib.executionHash(assertion);

            // memoryFrame.deadlineBlock = uint64(block.number) + confirmPeriodBlocks;

            memoryFrame.notFirstChild = memoryFrame.prevAssertion.firstChildCreationBlock > 0;

            // TODO: is it ok to just set last hash to prev since regardless?
            memoryFrame.lastHash = memoryFrame.prevAssertion.nodeHash;
            // // here we don't use ternacy operator to remain compatible with slither
            // if (!memoryFrame.notFirstChild) {
            //     memoryFrame.lastHash = getAssertionStorage(memoryFrame.prevAssertion.latestChildNumber)
            //         .nodeHash;
            // } else {
            //     memoryFrame.lastHash = memoryFrame.prevAssertion.nodeHash;
            // }

            newAssertionHash = AssertionLib.assertionHash({
                notFirstChild: memoryFrame.notFirstChild,
                lastHash: memoryFrame.lastHash,
                assertionExecHash: memoryFrame.executionHash,
                inboxAcc: memoryFrame.sequencerBatchAcc,
                wasmModuleRoot: wasmModuleRoot
            });
            require(
                newAssertionHash == inputs.expectedAssertionHash || inputs.expectedAssertionHash == bytes32(0),
                "UNEXPECTED_NODE_HASH"
            );

            memoryFrame.assertion = AssertionLib.createAssertion({
                _inputs: inputs,
                _inboxMaxCount: memoryFrame.currentInboxSize
            });
            // memoryFrame.node = NodeLib.createNode(
            //     RollupLib.stateHash(assertion.afterState, memoryFrame.currentInboxSize),
            //     RollupLib.challengeRootHash(
            //         memoryFrame.executionHash,
            //         block.number,
            //         wasmModuleRoot
            //     ),
            //     RollupLib.confirmHash(assertion),
            //     prevNodeNum,
            //     memoryFrame.deadlineBlock,
            //     newNodeHash
            // );
        }

        {
            uint64 assertionNum = latestAssertionCreated() + 1;

            require(_assertionToNum[memoryFrame.assertion.stateHash] == 0, "ASSERTION_SEEN");
            _assertionToNum[memoryFrame.assertion.stateHash] = assertionNum;

            // Fetch a storage reference to prevNode since we copied our other one into memory
            // and we don't have enough stack available to keep to keep the previous storage reference around
            Assertion storage prevAssertion = getAssertionStorage(inputs.prevNum);
            prevAssertion.childCreated(assertionNum);

            assertionCreated(memoryFrame.assertion);
        }

        // TODO: fix event
        emit AssertionCreated(
            latestAssertionCreated(),
            memoryFrame.prevAssertion.nodeHash,
            newAssertionHash,
            memoryFrame.executionHash,
            inputs,
            memoryFrame.sequencerBatchAcc,
            wasmModuleRoot,
            memoryFrame.currentInboxSize
        );

        return newAssertionHash;
    }
}
