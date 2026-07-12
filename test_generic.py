import requests
import asyncio
import websockets
import json
import sys

BASE_URL = "http://localhost:8080"
WS_URL = "ws://localhost:8080"

async def run_agent(agent_name):
    # 1. Create terminal
    print(f"==================================================")
    print(f"Creating terminal for {agent_name}...")
    res = requests.post(f"{BASE_URL}/api/terminals", json={
        "cwd": "/root/caw",
        "cmd": [agent_name]
    })
    res.raise_for_status()
    term_id = res.json()["data"]["id"]
    print(f"Terminal created with ID: {term_id}")

    # 2. Connect to terminal WS
    ws_uri = f"{WS_URL}/ws/terminals/{term_id}"
    print(f"Connecting to terminal WS: {ws_uri}")
    try:
        async with websockets.connect(ws_uri) as ws:
            # Read task to print terminal output
            async def read_output():
                try:
                    async for message in ws:
                        data = json.loads(message)
                        if data["type"] == "output":
                            print("TERMINAL OUTPUT:", repr(data["data"]))
                except websockets.exceptions.ConnectionClosed:
                    print("WS Connection Closed")

            asyncio.create_task(read_output())

            # Wait for startup output
            await asyncio.sleep(5)
            
            # Poll statuses
            print("Polling statuses initially...")
            status_res = requests.get(f"{BASE_URL}/api/agents/statuses")
            print("Initial statuses:", json.dumps(status_res.json(), indent=2))
            
            # Send "hi\r"
            print("Sending 'hi' to agent...")
            await ws.send(json.dumps({
                "type": "input",
                "data": "hi\r"
            }))
            
            # Poll statuses for the next 15 seconds every 500ms
            for _ in range(30):
                await asyncio.sleep(0.5)
                status_res = requests.get(f"{BASE_URL}/api/agents/statuses")
                for agent_status in status_res.json()["data"]:
                    if agent_status["sessionId"] == term_id:
                        print(f"Status: {agent_status['status']} | title: {agent_status.get('title')} | tool: {agent_status.get('tool')}")

    except Exception as e:
        print(f"Error during run: {e}")
    finally:
        # Close terminal
        print("Deleting terminal...")
        requests.delete(f"{BASE_URL}/api/terminals/{term_id}")

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python3 test_generic.py <agent_name>")
        sys.exit(1)
    asyncio.run(run_agent(sys.argv[1]))
