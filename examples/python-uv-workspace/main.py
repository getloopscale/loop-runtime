from flask import Flask, jsonify
from workspace_package import hello

app = Flask(__name__)


@app.route("/")
def index():
    return jsonify({"message": hello()})


@app.route("/health")
def health():
    return jsonify({"status": "healthy"})


if __name__ == "__main__":
    import os

    port = int(os.environ.get("PORT", 5000))
    app.run(host="0.0.0.0", port=port)
