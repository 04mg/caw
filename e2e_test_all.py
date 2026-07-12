import requests
import asyncio
import websockets
import json
import time

BASE_URL = "http://localhost:8080"
WS_URL = "ws://localhost:8080"

AGENTS = {
    "claude": ['claude', '--dangerously-skip-permissions'],
    "codex": ['codex', '--sandbox', 'workspace-write', '--ask-for-approval', 'never'],
    "copilot": ['copilot', '--allow-all-tools', '--allow-all-paths'],
    "agy": ['agy', '--dangerously-skip-permissions'],
    "opencode": ['opencode', '--dangerously-skip-permissions'],
    "pi": ['pi']
}

async def run_happy_path(agent_name):
    cmd = AGENTS[agent_name]
    print(f"\n==================================================")
    print(f"TESTING HAPPY PATH FOR {agent_name}...")
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
            # Task to gather/print output in background
            async def read_output():
                try:
                    async for message in ws:
                        pass
                except websockets.exceptions.ConnectionClosed:
                    pass
            asyncio.create_task(read_output())

            # Wait for agent boot
            await asyncio.sleep(5)
            
            # Initial status
            status_res = requests.get(f"{BASE_URL}/api/agents/statuses")
            print("Initial statuses:", json.dumps(status_res.json()["data"], indent=2))
            
            # Send "hi\r"
            print("Sending 'hi' to agent...")
            await ws.send(json.dumps({
                "type": "input",
                "data": "hi\r"
            }))
            
            # Poll status transitions
            history = []
            for _ in range(30):
                await asyncio.sleep(0.5)
                status_res = requests.get(f"{BASE_URL}/api/agents/statuses")
                for s in status_res.json()["data"]:
                    if s["sessionId"] == term_id:
                        status_str = f"{s['status']} (title: {s.get('title')})"
                        if not history or history[-1] != status_str:
                            history.append(status_str)
                            print(f"Transition: {status_str}")

            print(f"Final status history for {agent_name}: {history}")

    except Exception as e:
        print(f"Error for {agent_name}: {e}")
    finally:
        print(f"Deleting terminal {term_id}...")
        requests.delete(f"{BASE_URL}/api/terminals/{term_id}")

async def main():
    for name in AGENTS:
        await run_happy_path(name)

if __name__ == "__main__":
    asyncio.run(main())
