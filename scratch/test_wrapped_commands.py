import requests
import asyncio
import json
import websockets
import sys

BASE_URL = "http://localhost:8080"
WS_URL = "ws://localhost:8080"

AGENTS = ["claude", "codex", "copilot", "agy", "opencode", "pi"]

async def test_agent(agent_name):
    print(f"\n================= Testing {agent_name} =================")
    payload = {
        "cwd": "/root/caw",
        "cmd": [agent_name]
    }
    if agent_name == "claude":
        payload["cmd"] = ["claude"]
    elif agent_name == "codex":
        payload["cmd"] = ["codex", "--sandbox", "workspace-write", "--ask-for-approval", "never"]
    elif agent_name == "opencode":
        payload["cmd"] = ["opencode", "--auto"]
    elif agent_name == "agy":
        payload["cmd"] = ["agy"]
    elif agent_name == "copilot":
        payload["cmd"] = ["copilot"]

    res = requests.post(f"{BASE_URL}/api/terminals", json=payload)
    if res.status_code != 200:
        print(f"Failed to start terminal for {agent_name}: {res.text}")
        return False
        
    term_id = res.json()["data"]["id"]
    print(f"Started terminal {term_id} for {agent_name}")
    
    try:
        async with websockets.connect(f"{WS_URL}/ws/terminals/{term_id}") as ws:
            output_received = []
            async def read_loop():
                try:
                    async for message in ws:
                        evt = json.loads(message)
                        if evt.get("type") == "output":
                            data = evt.get("data", "")
                            output_received.append(data)
                            sys.stdout.write(data)
                            sys.stdout.flush()
                except Exception as e:
                    print(f"Exception in read_loop: {e}", file=sys.stderr)
            asyncio.create_task(read_loop())
            
            boot_time = 15 if agent_name in ["copilot", "claude", "agy", "opencode"] else 5
            print(f"Waiting {boot_time}s for boot...")
            await asyncio.sleep(boot_time)
            
            if agent_name == "copilot":
                print("\nSending Enter key for copilot trust...")
                await ws.send(json.dumps({"type": "input", "data": "\r"}))
                await asyncio.sleep(5)
                
            target_title = f"Clean Title For {agent_name}"
            prompt_text = f"<command-name>/plan</command-name> <command-message>plan</command-message> <command-args>{target_title}</command-args>"
            
            print(f"\nSending prompt text: {prompt_text!r}")
            await ws.send(json.dumps({"type": "input", "data": prompt_text}))
            
            await asyncio.sleep(1.0)
            
            print("\nSending Carriage Return...")
            await ws.send(json.dumps({"type": "input", "data": "\r"}))
            
            success = False
            for _ in range(40):
                await asyncio.sleep(0.5)
                status_res = requests.get(f"{BASE_URL}/api/agents/statuses")
                if status_res.status_code == 200:
                    statuses = status_res.json()["data"]
                    for item in statuses:
                        agent_id = item.get("agentId", "")
                        session_id = item.get("sessionId", "")
                        title = item.get("title")
                        if agent_id == agent_name and session_id == term_id:
                            print(f"\n[{agent_name}] Status: {item.get('status')}, Title: {title!r}")
                            is_clean = (title == target_title) or (
                                agent_name == "opencode" and title and ("clean" in title.lower() or "title" in title.lower()) and "<" not in title and ">" not in title
                            )
                            if is_clean:
                                print(f"Success! Title for {agent_name} set to: {title!r}")
                                success = True
                                break
                    if success:
                        break
            
            if not success:
                print(f"\nFailed to find expected title {target_title!r} for {agent_name}")
                status_res = requests.get(f"{BASE_URL}/api/agents/statuses")
                print(f"Final statuses: {status_res.json()['data']}")
                return False
            return True
    finally:
        requests.delete(f"{BASE_URL}/api/terminals/{term_id}")

async def main():
    results = {}
    for agent in AGENTS:
        results[agent] = await test_agent(agent)
    print("\n================= TEST SUMMARY =================")
    for agent, ok in results.items():
        print(f"Agent {agent}: {'✅ SUCCESS' if ok else '❌ FAILED'}")

if __name__ == "__main__":
    asyncio.run(main())
