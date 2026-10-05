<script setup>
import { searchOpen } from '../lib/globalSearch'
import SearchModal from './SearchModal.vue'
</script>

<template>
  <SearchModal v-model="searchOpen" />
</template>
