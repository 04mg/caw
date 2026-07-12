import requests
import asyncio
import websockets
import json

BASE_URL = "http://localhost:8080"
WS_URL = "ws://localhost:8080"

async def main():
    res = requests.post(f"{BASE_URL}/api/terminals", json={
        "cwd": "/root/caw",
        "cmd": ["claude"]
    })
    res.raise_for_status()
    term_id = res.json()["data"]["id"]
    print(f"Terminal created: {term_id}")
    
    ws_uri = f"{WS_URL}/ws/terminals/{term_id}"
    async with websockets.connect(ws_uri) as ws:
        await ws.send(json.dumps({"type": "resize", "cols": 120, "rows": 40}))
        
        async def read_output():
            try:
                async for msg in ws:
                    data = json.loads(msg)
                    if data["type"] == "output":
                        print(f"[TTY] {repr(data['data'])}")
            except:
                pass
        asyncio.create_task(read_output())
        
        await asyncio.sleep(5)
        print("Sending prompt: 'run command: sleep 12'...")
        await ws.send(json.dumps({"type": "input", "data": "run command: sleep 12\r"}))
        
        # Poll status
        for i in range(40):
            await asyncio.sleep(0.5)
            s_res = requests.get(f"{BASE_URL}/api/agents/statuses")
            for s in s_res.json()["data"]:
                if s["sessionId"] == term_id:
                    print(f"[STATUS {i*0.5}s] {s['status']} | tool: {s.get('tool')} | title: {s.get('title')}")
            
            # Send approval 'y' after 6 seconds
            if i == 12:
                print("Sending 'y' to approve...")
                await ws.send(json.dumps({"type": "input", "data": "y\r"}))

    requests.delete(f"{BASE_URL}/api/terminals/{term_id}")

if __name__ == "__main__":
    asyncio.run(main())
