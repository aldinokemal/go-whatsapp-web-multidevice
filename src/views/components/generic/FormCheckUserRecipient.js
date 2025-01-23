export default {
    name: 'FormCheckUserRecipient',
    props: {
        type: {
            type: String,
            required: true
        },
        phone: {
            type: String,
            required: true
        },
    },
    data() {
        return {
            recipientTypes: []
        };
    },
    computed: {
        phone_id() {
            return this.phone + this.type;
        }
    },
    mounted() {
        this.recipientTypes = [
            { value: window.TYPEUSER, text: 'Private Message' },
        ];
    },
    methods: {
        updateType(event) {
            this.$emit('update:type', event.target.value);
        },
        updatePhone(event) {
            this.$emit('update:phone', event.target.value);
        }
    },
    template: `
    <div class="field">
        <label>Type</label>
        <select name="type" @change="updateType" class="ui dropdown">
            <option v-for="type in recipientTypes" :value="type.value">{{ type.text }}</option>
        </select>
    </div>
    
    <div class="field">
        <label>Phone</label>
        <input :value="phone" aria-label="wa identifier" @input="updatePhone">
        <input :value="phone_id" disabled aria-label="whatsapp_id">
    </div>
    `
}