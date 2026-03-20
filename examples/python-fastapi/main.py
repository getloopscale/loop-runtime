from fastapi import FastAPI

app = FastAPI(title="FastAPI Example", version="1.0.0")


@app.get("/")
async def root():
    return {"message": "Hello from FastAPI", "version": "1.0.0"}


@app.get("/health")
async def health():
    return {"status": "healthy", "service": "fastapi-app"}
