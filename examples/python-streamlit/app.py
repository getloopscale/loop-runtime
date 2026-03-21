import streamlit as st
import pandas as pd

st.set_page_config(page_title="Streamlit App", page_icon="📊")

st.title("Streamlit App")
st.write("This is a Streamlit dashboard deployed with LoopZero.")

data = pd.DataFrame({"x": range(10), "y": [i**2 for i in range(10)]})
st.line_chart(data.set_index("x"))

st.metric("Status", "Running", delta="OK")
