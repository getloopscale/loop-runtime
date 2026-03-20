from flask import Flask

app = Flask(__name__)


@app.route('/')
def index():
    return {'status': 'ok', 'app': 'myapp'}


@app.route('/health')
def health():
    return {'healthy': True}
