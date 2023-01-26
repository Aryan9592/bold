// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.13;

import "forge-std/Test.sol";

import "../src/rollup/RollupProxy.sol";

import "../src/rollup/RollupCore.sol";
import "../src/rollup/RollupUserLogic.sol";
import "../src/rollup/RollupAdminLogic.sol";
import "../src/rollup/RollupCreator.sol";

import "../src/osp/OneStepProver0.sol";
import "../src/osp/OneStepProverMemory.sol";
import "../src/osp/OneStepProverMath.sol";
import "../src/osp/OneStepProverHostIo.sol";
import "../src/osp/OneStepProofEntry.sol";
import "../src/challenge/ChallengeManager.sol";

contract RollupTest is Test {
    address constant owner = address(1337);
    address constant sequencer = address(7331);

    address constant validator1 = address(100001);
    address constant validator2 = address(100002);
    address constant validator3 = address(100003);

    bytes32 constant wasmModuleRoot = keccak256("wasmModuleRoot");
    uint256 constant BASE_STAKE = 10;
    uint256 constant CONFIRM_PERIOD_BLOCKS = 100;

    bytes32 constant FIRST_ASSERTION_BLOCKHASH = keccak256("FIRST_ASSERTION_BLOCKHASH");
    bytes32 constant FIRST_ASSERTION_SENDROOT = keccak256("FIRST_ASSERTION_SENDROOT");

    RollupProxy rollup;
    RollupUserLogic userRollup;
    RollupAdminLogic adminRollup;

    address[] validators;
    bool[] flags;

    event RollupCreated(
        address indexed rollupAddress,
        address inboxAddress,
        address adminProxy,
        address sequencerInbox,
        address bridge
    );

    function setUp() public {
        OneStepProver0 oneStepProver = new OneStepProver0();
        OneStepProverMemory oneStepProverMemory = new OneStepProverMemory();
        OneStepProverMath oneStepProverMath = new OneStepProverMath();
        OneStepProverHostIo oneStepProverHostIo = new OneStepProverHostIo();
        OneStepProofEntry oneStepProofEntry = new OneStepProofEntry(
            oneStepProver, oneStepProverMemory, oneStepProverMath, oneStepProverHostIo);
        ChallengeManager challengeManagerImpl = new ChallengeManager();
        BridgeCreator bridgeCreator = new BridgeCreator();
        RollupCreator rollupCreator = new RollupCreator();
        RollupAdminLogic rollupAdminLogicImpl = new RollupAdminLogic();
        RollupUserLogic rollupUserLogicImpl = new RollupUserLogic();

        rollupCreator.setTemplates(
            bridgeCreator,
            oneStepProofEntry,
            challengeManagerImpl,
            rollupAdminLogicImpl,
            rollupUserLogicImpl,
            address(0),
            address(0)
        );

        Config memory config = Config({
            baseStake: BASE_STAKE,
            chainId: 0,
            confirmPeriodBlocks: uint64(CONFIRM_PERIOD_BLOCKS),
            extraChallengeTimeBlocks: 100,
            owner: owner,
            sequencerInboxMaxTimeVariation: ISequencerInbox.MaxTimeVariation({
                delayBlocks: (60 * 60 * 24) / 15,
                futureBlocks: 12,
                delaySeconds: 60 * 60 * 24,
                futureSeconds: 60 * 60
            }),
            stakeToken: address(0),
            wasmModuleRoot: wasmModuleRoot,
            loserStakeEscrow: address(0),
            genesisBlockNum: 0
        });

        address expectedRollupAddr = address(uint160(uint256(keccak256(abi.encodePacked(bytes1(0xd6), bytes1(0x94), address(rollupCreator), bytes1(0x03))))));

        vm.expectEmit(true, true, false, false);
        emit RollupCreated(expectedRollupAddr, address(0), address(0), address(0), address(0));
        rollupCreator.createRollup(config, expectedRollupAddr);

        userRollup = RollupUserLogic(address(expectedRollupAddr));
        adminRollup = RollupAdminLogic(address(expectedRollupAddr));

        vm.startPrank(owner);
        validators.push(validator1);
        validators.push(validator2);
        validators.push(validator3);
        flags.push(true);
        flags.push(true);
        flags.push(true);
        adminRollup.setValidator(address[](validators), flags);
        adminRollup.sequencerInbox().setIsBatchPoster(sequencer, true);
        vm.stopPrank();

        payable(validator1).transfer(1 ether);
        payable(validator2).transfer(1 ether);
        payable(validator3).transfer(1 ether);

        vm.roll(block.number + 75); 
    }

    function _createNewBatch() internal returns (uint256){
        uint256 count = userRollup.bridge().sequencerMessageCount();
        vm.startPrank(sequencer);
        userRollup.sequencerInbox().addSequencerL2Batch({
            sequenceNumber: count, 
            data: "", 
            afterDelayedMessagesRead : 1, 
            gasRefunder: IGasRefunder(address(0)), 
            prevMessageCount: 0, 
            newMessageCount: 0
        });
        vm.stopPrank();
        assertEq(userRollup.bridge().sequencerMessageCount(), ++count);
        return count;
    }

    function testSuccessCreateAssertions() public {
        uint64 inboxcount = uint64(_createNewBatch());
        ExecutionState memory beforeState;
        beforeState.machineStatus = MachineStatus.FINISHED;
        ExecutionState memory afterState;
        afterState.machineStatus = MachineStatus.FINISHED;
        afterState.globalState.bytes32Vals[0] = FIRST_ASSERTION_BLOCKHASH; // blockhash
        afterState.globalState.bytes32Vals[1] = FIRST_ASSERTION_SENDROOT; // sendroot
        afterState.globalState.u64Vals[0] = 1; // inbox count
        afterState.globalState.u64Vals[1] = 0; // pos in msg
        vm.prank(validator1);
        userRollup.newStakeOnNewAssertion{value: BASE_STAKE}(NewAssertionInputs({
            beforeState: beforeState,
            afterState: afterState,
            numBlocks: 1,
            prevNum: 0,
            prevStateCommitment: bytes32(0),
            prevNodeInboxMaxCount: 1,
            expectedAssertionHash: bytes32(0)
        }));

        ExecutionState memory afterState2;
        afterState2.machineStatus = MachineStatus.FINISHED;
        afterState2.globalState.u64Vals[0] = inboxcount;
        vm.roll(block.number + 75); 
        vm.prank(validator1);
        userRollup.stakeOnNewAssertion(NewAssertionInputs({
            beforeState: afterState,
            afterState: afterState2,
            numBlocks: 1,
            prevNum: 1,
            prevStateCommitment: bytes32(0),
            prevNodeInboxMaxCount: inboxcount,
            expectedAssertionHash: bytes32(0)
        }));
    }

    function testRevertIdentialAssertions() public {
        uint64 inboxcount = uint64(_createNewBatch());
        ExecutionState memory beforeState;
        beforeState.machineStatus = MachineStatus.FINISHED;
        ExecutionState memory afterState;
        afterState.machineStatus = MachineStatus.FINISHED;
        afterState.globalState.bytes32Vals[0] = FIRST_ASSERTION_BLOCKHASH; // blockhash
        afterState.globalState.bytes32Vals[1] = FIRST_ASSERTION_SENDROOT; // sendroot
        afterState.globalState.u64Vals[0] = 1; // inbox count
        afterState.globalState.u64Vals[1] = 0; // pos in msg

        NewAssertionInputs memory inputs = NewAssertionInputs({
            beforeState: beforeState,
            afterState: afterState,
            numBlocks: 1,
            prevNum: 0,
            prevStateCommitment: bytes32(0),
            prevNodeInboxMaxCount: 1,
            expectedAssertionHash: bytes32(0)
        });

        vm.prank(validator1);
        userRollup.newStakeOnNewAssertion{value: BASE_STAKE}(inputs);

        vm.prank(validator2);
        vm.expectRevert("ASSERTION_SEEN");
        userRollup.newStakeOnNewAssertion{value: BASE_STAKE}(inputs);
    }

    function testRevertAssertWrongBranch() public {
        uint64 inboxcount = uint64(_createNewBatch());
        ExecutionState memory beforeState;
        beforeState.machineStatus = MachineStatus.FINISHED;
        ExecutionState memory afterState;
        afterState.machineStatus = MachineStatus.FINISHED;
        afterState.globalState.bytes32Vals[0] = FIRST_ASSERTION_BLOCKHASH; // blockhash
        afterState.globalState.bytes32Vals[1] = FIRST_ASSERTION_SENDROOT; // sendroot
        afterState.globalState.u64Vals[0] = 1; // inbox count
        afterState.globalState.u64Vals[1] = 0; // pos in msg

        vm.prank(validator1);
        userRollup.newStakeOnNewAssertion{value: BASE_STAKE}(NewAssertionInputs({
            beforeState: beforeState,
            afterState: afterState,
            numBlocks: 1,
            prevNum: 0,
            prevStateCommitment: bytes32(0),
            prevNodeInboxMaxCount: 1,
            expectedAssertionHash: bytes32(0)
        }));

        vm.expectRevert("WRONG_BRANCH");
        afterState.globalState.u64Vals[1] = 1; // modify the state
        vm.roll(block.number + 75); 
        vm.prank(validator1);
        userRollup.stakeOnNewAssertion(NewAssertionInputs({
            beforeState: beforeState,
            afterState: afterState,
            numBlocks: 1,
            prevNum: 0,
            prevStateCommitment: bytes32(0),
            prevNodeInboxMaxCount: 1,
            expectedAssertionHash: bytes32(0)
        }));
    }

    function testSuccessCreateSecondChild() public {
        uint64 inboxcount = uint64(_createNewBatch());
        ExecutionState memory beforeState;
        beforeState.machineStatus = MachineStatus.FINISHED;
        ExecutionState memory afterState;
        afterState.machineStatus = MachineStatus.FINISHED;
        afterState.globalState.bytes32Vals[0] = FIRST_ASSERTION_BLOCKHASH; // blockhash
        afterState.globalState.bytes32Vals[1] = FIRST_ASSERTION_SENDROOT; // sendroot
        afterState.globalState.u64Vals[0] = 1; // inbox count
        afterState.globalState.u64Vals[1] = 0; // pos in msg

        vm.prank(validator1);
        userRollup.newStakeOnNewAssertion{value: BASE_STAKE}(NewAssertionInputs({
            beforeState: beforeState,
            afterState: afterState,
            numBlocks: 1,
            prevNum: 0,
            prevStateCommitment: bytes32(0),
            prevNodeInboxMaxCount: 1,
            expectedAssertionHash: bytes32(0)
        }));

        afterState.globalState.u64Vals[1] = 1; // modify the state
        vm.prank(validator2);
        userRollup.newStakeOnNewAssertion{value: BASE_STAKE}(NewAssertionInputs({
            beforeState: beforeState,
            afterState: afterState,
            numBlocks: 1,
            prevNum: 0,
            prevStateCommitment: bytes32(0),
            prevNodeInboxMaxCount: 1,
            expectedAssertionHash: bytes32(0)
        }));

        assertEq(userRollup.getAssertion(0).secondChildCreationBlock, block.number);
    }

    function testRevertConfirmWrongInput() public {
        testSuccessCreateAssertions();
        vm.roll(userRollup.getAssertion(0).firstChildCreationBlock + CONFIRM_PERIOD_BLOCKS + 1);
        vm.prank(validator1);
        vm.expectRevert("CONFIRM_DATA");
        userRollup.confirmNextAssertion(bytes32(0), bytes32(0));
    }

    function testSuccessConfirmUnchallengedAssertions() public {
        testSuccessCreateAssertions();
        vm.roll(userRollup.getAssertion(0).firstChildCreationBlock + CONFIRM_PERIOD_BLOCKS + 1);
        vm.prank(validator1);
        userRollup.confirmNextAssertion(FIRST_ASSERTION_BLOCKHASH, FIRST_ASSERTION_SENDROOT);
    }

    function testRevertConfirmChallengedAssertions() public {
        testSuccessCreateSecondChild();
        vm.roll(userRollup.getAssertion(0).firstChildCreationBlock + CONFIRM_PERIOD_BLOCKS + 1);
        vm.prank(validator1);
        vm.expectRevert("IN_CHAL");
        userRollup.confirmNextAssertion(FIRST_ASSERTION_BLOCKHASH, FIRST_ASSERTION_SENDROOT);
    }
}

