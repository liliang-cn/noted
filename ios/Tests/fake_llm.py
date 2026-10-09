"""A stand-in for an OpenAI-compatible model, for UI tests. It answers by keyword so
the app's AI screens can be driven without a real model."""
import http.server, json, os, re, sys
class H(http.server.BaseHTTPRequestHandler):
    def log_message(self,*a): pass
    def send(self, obj):
        b=json.dumps(obj).encode(); self.send_response(200); self.send_header('content-type','application/json'); self.send_header('content-length',str(len(b))); self.end_headers(); self.wfile.write(b)
    def do_GET(self): self.send({"object":"list","data":[{"id":"fake-model","object":"model"}]})
    def do_POST(self):
        body=self.rfile.read(int(self.headers.get('content-length',0))).decode()
        # A test can tag a question with PRIV<digits>; everything the model is sent for it is kept,
        # so the test can check what did and did not reach the model.
        for tag in set(re.findall(r'PRIV\d+', body)):
            os.makedirs('/tmp/noted-fake-llm', exist_ok=True)
            with open('/tmp/noted-fake-llm/%s.log' % tag, 'a') as f:
                f.write(body + '\n')
        req=json.loads(body)
        msgs=req.get('messages',[])
        users=[m for m in msgs if m.get('role')=='user']
        last=users[-1]['content'] if users else ''
        if isinstance(last,list): last=' '.join(x.get('text','') for x in last)
        has_tool=any(m.get('role')=='tool' for m in msgs)
        def msg(content=None, calls=None):
            m={"role":"assistant","content":content}
            if calls: m["tool_calls"]=calls
            self.send({"id":"x","object":"chat.completion","model":"fake-model","choices":[{"index":0,"finish_reason":"tool_calls" if calls else "stop","message":m}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}})
        if 'break it into ONE project' in body:
            return msg(json.dumps({"project":"UIT 东京行","start":None,"due":None,"space":"life","tasks":[{"title":"订机票","days_before_start":3},{"title":"办理签证","days_before_start":2},{"title":"准备酒店","days_before_start":1}]},ensure_ascii=False))
        if 'author of this note' in body:
            return msg(json.dumps({"tasks":[{"title":"订大阪机票","due":"2026-10-31"},{"title":"订酒店,靠近难波","due":None},{"title":"办电子签","due":None}]},ensure_ascii=False))
        tools={t['function']['name'] for t in req.get('tools',[])}
        if not has_tool and ('创建' in last or 'create' in last.lower()) and 'create_task' in tools:
            return msg(None,[{"id":"c1","type":"function","function":{"name":"create_task","arguments":json.dumps({"title":"买牛奶"},ensure_ascii=False)}}])
        if not has_tool and ('待办' in last or 'todo' in last.lower()) and 'list_tasks' in tools:
            return msg(None,[{"id":"c2","type":"function","function":{"name":"list_tasks","arguments":"{}"}}])
        return msg("好的,已经看过了。" if has_tool else "你好,我在。")
http.server.HTTPServer(('127.0.0.1',int(sys.argv[1]) if len(sys.argv)>1 else 43911),H).serve_forever()
