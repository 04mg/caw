import requests
import asyncio
import websockets
import json
import sys

BASE_URL = "http://localhost:8080"
WS_URL = "ws://localhost:8080"

AGENTS = {
    "claude": ['claude', '--dangerously-skip-permissions'],
    "codex": ['codex', '--sandbox', 'workspace-write', '--ask-for-approval', 'never', 'hi'],
    "copilot": ['copilot', '--allow-all-tools', '--allow-all-paths'],
    "agy": ['agy', '--dangerously-skip-permissions'],
    "opencode": ['opencode', '--dangerously-skip-permissions', '--prompt', 'hi'],
    "pi": ['pi', 'hi']
}

async def run_happy_path(agent_name):
    cmd = AGENTS[agent_name]
    print(f"\n==================================================")
    print(f"TESTING REAL-TIME TRANSITIONS FOR {agent_name}...")
    
    transitions = []
    stop_polling = False
    term_id = None

    async def poll_status_loop():
        nonlocal term_id
        while not stop_polling:
            if term_id:
                try:
                    res = requests.get(f"{BASE_URL}/api/agents/statuses")
                    if res.status_code == 200:
                        for s in res.json()["data"]:
                            if s["sessionId"] == term_id:
                                state_str = f"{s['status']} | title: {s.get('title')} | tool: {s.get('tool')}"
                                if not transitions or transitions[-1] != state_str:
                                    transitions.append(state_str)
                                    print(f"[TRANSITION] {state_str}")
                except Exception:
                    pass
            await asyncio.sleep(0.1)

    polling_task = asyncio.create_task(poll_status_loop())

    res = requests.post(f"{BASE_URL}/api/terminals", json={
        "cwd": "/root/caw",
        "cmd": cmd
    })
    res.raise_for_status()
    term_id = res.json()["data"]["id"]
    print(f"Terminal created with ID: {term_id}")

    ws_uri = f"{WS_URL}/ws/terminals/{term_id}"
    print(f"Connecting to terminal WS...")
    
    try:
        async with websockets.connect(ws_uri) as ws:
            # Send resize
            await ws.send(json.dumps({
                "type": "resize",
                "cols": 120,
                "rows": 40
            }))

            # Task to gather/print output in background
            async def read_output():
                try:
                    async for message in ws:
                        data = json.loads(message)
                        if data["type"] == "output":
                            print(f"[{agent_name} TTY] {repr(data['data'])}")
                except websockets.exceptions.ConnectionClosed:
                    pass
            asyncio.create_task(read_output())

            # 1. Wait until status becomes idle with title 'hi'
            print("Waiting for agent to become idle...")
            for _ in range(30):
                await asyncio.sleep(0.5)
                # Check current status
                res = requests.get(f"{BASE_URL}/api/agents/statuses")
                is_idle = False
                for s in res.json()["data"]:
                    if s["sessionId"] == term_id and s["status"] == "idle" and s.get("title") == "hi":
                        is_idle = True
                        break
                if is_idle:
                    break
            
            print("Agent is idle. Sending second prompt 'hello'...")
            # Send "hello\r"
            await ws.send(json.dumps({
                "type": "input",
                "data": "hello\r"
            }))
            
            # Wait 10 seconds to capture the transition
            await asyncio.sleep(10)

    except Exception as e:
        print(f"Error for {agent_name}: {e}")
    finally:
        stop_polling = True
        await polling_task
        print(f"Final status history for {agent_name}: {transitions}")
        print(f"Deleting terminal {term_id}...")
        requests.delete(f"{BASE_URL}/api/terminals/{term_id}")

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python3 test_step_by_step.py <agent_name>")
        sys.exit(1)
    asyncio.run(run_happy_path(sys.argv[1]))
