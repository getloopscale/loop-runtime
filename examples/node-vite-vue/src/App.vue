<script setup>
import { ref } from "vue";

const count = ref(0);
</script>

<template>
  <div>
    <h1>Vite + Vue</h1>
    <button @click="count++">Count: {{ count }}</button>
    <p>Edit src/App.vue and save to test HMR.</p>
  </div>
</template>
