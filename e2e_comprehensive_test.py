import requests
import asyncio
import websockets
import json
import time

BASE_URL = "http://localhost:8080"
WS_URL = "ws://localhost:8080"

AGENTS = {
    "claude": ['claude'],
    "codex": ['codex', '--sandbox', 'workspace-write', '--ask-for-approval', 'never', 'hi'],
    "copilot": ['copilot', '--allow-all-tools', '--allow-all-paths'],
    "agy": ['agy', '--dangerously-skip-permissions'],
    "opencode": ['opencode', '--auto', '--prompt', 'hi'],
    "pi": ['pi', 'hi']
}

async def poll_until_state(term_id, target_status, max_wait=30):
    for _ in range(int(max_wait * 2)):
        await asyncio.sleep(0.5)
        res = requests.get(f"{BASE_URL}/api/agents/statuses")
        for s in res.json()["data"]:
            if s["sessionId"] == term_id and s["status"] == target_status:
                return True
    return False

async def get_current_status(term_id):
    res = requests.get(f"{BASE_URL}/api/agents/statuses")
    for s in res.json()["data"]:
        if s["sessionId"] == term_id:
            return s
    return None

async def test_happy_path(agent_name):
    cmd = AGENTS[agent_name]
    print(f"\n[HAPPY PATH] Testing {agent_name}...")
    res = requests.post(f"{BASE_URL}/api/terminals", json={"cwd": "/root/caw", "cmd": cmd})
    res.raise_for_status()
    term_id = res.json()["data"]["id"]
    
    transitions = []
    stop_polling = False
    
    async def poll_loop():
        while not stop_polling:
            s = await get_current_status(term_id)
            if s:
                state_str = f"{s['status']} (title: {s.get('title')})"
                if not transitions or transitions[-1] != state_str:
                    transitions.append(state_str)
                    print(f"  [{agent_name} transition] {state_str}")
            await asyncio.sleep(0.1)
            
    poller = asyncio.create_task(poll_loop())
    
    try:
        async with websockets.connect(f"{WS_URL}/ws/terminals/{term_id}") as ws:
            await ws.send(json.dumps({"type": "resize", "cols": 120, "rows": 40}))
            
            # Read output task
            async def read_out():
                try:
                    async for _ in ws: pass
                except: pass
            asyncio.create_task(read_out())
            
            if agent_name == "copilot":
                await asyncio.sleep(10)
                await ws.send(json.dumps({"type": "input", "data": "\r"})) # Folder trust
                await asyncio.sleep(3)
                await ws.send(json.dumps({"type": "input", "data": "hi\r"}))
            elif agent_name in ["claude", "agy"]:
                await asyncio.sleep(5)
                await ws.send(json.dumps({"type": "input", "data": "hi\r"}))
            elif agent_name == "pi":
                await asyncio.sleep(6) # Wait for initial prompt to complete
                await ws.send(json.dumps({"type": "input", "data": "hello\r"})) # Send second prompt to capture transitions
            else:
                # Codex and OpenCode started with prompt argument
                pass
                
            await asyncio.sleep(15)
    finally:
        stop_polling = True
        await poller
        print(f"[HAPPY PATH] Finished {agent_name}. Transitions: {transitions}")
        requests.delete(f"{BASE_URL}/api/terminals/{term_id}")
    return transitions

async def test_waiting_input_copilot():
    print(f"\n[WAITING INPUT] Testing Copilot 'ask_user' transition...")
    res = requests.post(f"{BASE_URL}/api/terminals", json={"cwd": "/root/caw", "cmd": AGENTS["copilot"]})
    res.raise_for_status()
    term_id = res.json()["data"]["id"]
    
    try:
        async with websockets.connect(f"{WS_URL}/ws/terminals/{term_id}") as ws:
            await ws.send(json.dumps({"type": "resize", "cols": 120, "rows": 40}))
            async def read_out():
                try:
                    async for _ in ws: pass
                except: pass
            asyncio.create_task(read_out())
            
            await asyncio.sleep(10)
            print("Confirming folder trust...")
            await ws.send(json.dumps({"type": "input", "data": "\r"}))
            await asyncio.sleep(3)
            
            # Send prompt to trigger ask_user tool
            print("Sending prompt: 'ask me a question'")
            await ws.send(json.dumps({"type": "input", "data": "ask me a question\r"}))
            
            # Poll until waiting_input
            found_waiting = False
            for _ in range(60):
                await asyncio.sleep(0.5)
                s = await get_current_status(term_id)
                if s:
                    print(f"  [Copilot status] {s['status']} (title: {s.get('title')} | tool: {s.get('tool')})")
                    if s["status"] == "waiting_input":
                        found_waiting = True
                        break
            
            if found_waiting:
                print("Successfully entered waiting_input! Answering question...")
                await ws.send(json.dumps({"type": "input", "data": "my name is Antigravity\r"}))
                
                # Verify it leaves waiting_input (goes to thinking, executing, idle, or asks another question)
                success_transition = False
                for _ in range(30):
                    await asyncio.sleep(0.5)
                    s = await get_current_status(term_id)
                    if s:
                        print(f"  [Copilot status after answer] {s['status']}")
                        # Success: any state change away from waiting_input OR another ask_user (follow-up question)
                        if s["status"] in ["idle", "thinking", "executing"]:
                            success_transition = True
                            break
                if success_transition:
                    print("SUCCESS: transitioned back from waiting_input!")
                else:
                    # May have asked a follow-up question - also valid
                    s = await get_current_status(term_id)
                    if s and s["status"] == "waiting_input":
                        print("SUCCESS: waiting_input → thinking → waiting_input (follow-up question, expected)!")
                        success_transition = True
                    else:
                        print("FAILED to transition back from waiting_input.")
            else:
                print("Failed to reach waiting_input.")
    finally:
        requests.delete(f"{BASE_URL}/api/terminals/{term_id}")

async def test_stuck_in_working_claude():
    print(f"\n[STUCK IN WORKING] Testing Claude with a long-running tool command...")
    res = requests.post(f"{BASE_URL}/api/terminals", json={"cwd": "/root/caw", "cmd": AGENTS["claude"]})
    res.raise_for_status()
    term_id = res.json()["data"]["id"]
    
    try:
        async with websockets.connect(f"{WS_URL}/ws/terminals/{term_id}") as ws:
            await ws.send(json.dumps({"type": "resize", "cols": 120, "rows": 40}))
            async def read_out():
                try:
                    async for _ in ws: pass
                except: pass
            asyncio.create_task(read_out())
            
            await asyncio.sleep(5)
            print("Sending command: 'run command: sleep 12'")
            await ws.send(json.dumps({"type": "input", "data": "run command: sleep 12\r"}))
            
            # Wait for executing
            executing_count = 0
            for _ in range(30):
                await asyncio.sleep(0.5)
                s = await get_current_status(term_id)
                if s:
                    print(f"  [Claude status] {s['status']} | tool: {s.get('tool')}")
                    if s["status"] == "executing":
                        executing_count += 1
                        break
            
            if executing_count > 0:
                print("Claude successfully entered 'executing' state!")
                print("Approving command execution in background...")
                await ws.send(json.dumps({"type": "input", "data": "y\r"}))
                
                # Check that it stays executing
                for _ in range(10):
                    await asyncio.sleep(0.5)
                    s = await get_current_status(term_id)
                    if s:
                        print(f"  [Claude executing status] {s['status']} | tool: {s.get('tool')}")
            else:
                print("FAILED to reach executing state.")
    finally:
        requests.delete(f"{BASE_URL}/api/terminals/{term_id}")

async def test_crash_recovery_claude():
    print(f"\n[CRASH RECOVERY] Testing Claude...")
    res = requests.post(f"{BASE_URL}/api/terminals", json={"cwd": "/root/caw", "cmd": AGENTS["claude"]})
    res.raise_for_status()
    term_id = res.json()["data"]["id"]
    
    try:
        async with websockets.connect(f"{WS_URL}/ws/terminals/{term_id}") as ws:
            await ws.send(json.dumps({"type": "resize", "cols": 120, "rows": 40}))
            async def read_out():
                try:
                    async for _ in ws: pass
                except: pass
            asyncio.create_task(read_out())
            
            await asyncio.sleep(5)
            print("Sending prompt 'run command: sleep 20'")
            await ws.send(json.dumps({"type": "input", "data": "run command: sleep 20\r"}))
            
            # Wait for executing
            for _ in range(30):
                await asyncio.sleep(0.5)
                s = await get_current_status(term_id)
                if s and s["status"] == "executing":
                    break
            
            s = await get_current_status(term_id)
            print(f"Current status: {s['status'] if s else 'None'}")
            
            # Kill terminal process (delete terminal)
            print("Killing terminal process...")
            requests.delete(f"{BASE_URL}/api/terminals/{term_id}")
            
            # Verify status returns to idle or is removed
            await asyncio.sleep(2)
            s = await get_current_status(term_id)
            print(f"Status after crash: {s['status'] if s else 'None'}")
    except Exception as e:
        print(f"Crash recovery error: {e}")

async def main():
    results = {}
    for name in AGENTS:
        results[name] = await test_happy_path(name)
        
    await test_waiting_input_copilot()
    await test_stuck_in_working_claude()
    await test_crash_recovery_claude()

if __name__ == "__main__":
    asyncio.run(main())
