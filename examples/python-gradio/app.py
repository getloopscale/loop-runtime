import gradio as gr


def greet(name, intensity):
    return f"Hello, {name}! " + "!" * int(intensity)


demo = gr.Interface(
    fn=greet,
    inputs=["text", gr.Slider(value=2, minimum=1, maximum=10, step=1)],
    outputs=["text"],
    title="Gradio App",
    description="A simple Gradio app deployed with LoopZero.",
)

if __name__ == "__main__":
    demo.launch(server_name="0.0.0.0", server_port=7860)
