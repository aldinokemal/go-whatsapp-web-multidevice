export default {
    name: 'GroupSetPhoto',
    data() {
        return {
            loading: false,
            groupId: '',
            photo: null,
            photoFile: null,
            previewUrl: null,
        }
    },
    methods: {
        openModal() {
            $('#modalGroupSetPhoto').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        isValidForm() {
            return this.groupId.trim() !== '';
        },
        handleFileChange(event) {
            const file = event.target.files[0];
            if (file) {
                this.photoFile = file;
                // Create preview
                const reader = new FileReader();
                reader.onload = (e) => {
                    this.previewUrl = e.target.result;
                };
                reader.readAsDataURL(file);
            }
        },
        handleRemovePhoto() {
            this.photoFile = null;
            this.previewUrl = null;
            // Reset file input
            const fileInput = document.querySelector('#photoUpload');
            if (fileInput) {
                fileInput.value = '';
            }
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) {
                return;
            }
            try {
                let response = await this.submitApi()
                showSuccessInfo(response)
                $('#modalGroupSetPhoto').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                const formData = new FormData();
                formData.append('group_id', this.groupId);
                if (this.photoFile) {
                    formData.append('photo', this.photoFile);
                }

                let response = await window.http.post(`/group/photo`, formData, {
                    headers: {
                        'Content-Type': 'multipart/form-data'
                    }
                })
                this.handleReset();
                return response.data.message;
            } catch (error) {
                if (error.response) {
                    throw new Error(error.response.data.message);
                }
                throw new Error(error.message);
            } finally {
                this.loading = false;
            }
        },
        handleReset() {
            this.groupId = '';
            this.photoFile = null;
            this.previewUrl = null;
            const fileInput = document.querySelector('#photoUpload');
            if (fileInput) {
                fileInput.value = '';
            }
        },
    },
    template: `
    <div class="green card" @click="openModal" style="cursor: pointer">
        <div class="content">
            <a class="ui green right ribbon label">Group</a>
            <div class="header">Set Group Photo</div>
            <div class="description">
                Update or remove group profile picture
            </div>
        </div>
    </div>
    
    <!--  Modal Group Set Photo  -->
    <div class="ui small modal" id="modalGroupSetPhoto">
        <i class="close icon"></i>
        <div class="header">
            Set Group Photo
        </div>
        <div class="content">
            <form class="ui form">
                <div class="field">
                    <label>Group ID</label>
                    <input v-model="groupId" type="text"
                           placeholder="120363024512399999@g.us"
                           aria-label="Group ID">
                </div>
                
                <div class="field">
                    <label>Group Photo</label>
                    <input type="file" id="photoUpload" accept="image/*" @change="handleFileChange">
                    <small class="text">Select a JPEG image for best results. Leave empty to remove current photo.</small>
                </div>
                
                <div class="field" v-if="previewUrl">
                    <label>Preview</label>
                    <div style="display: flex; align-items: center; gap: 10px;">
                        <img :src="previewUrl" alt="Preview" style="width: 100px; height: 100px; object-fit: cover; border-radius: 8px;">
                        <button class="ui red button" @click="handleRemovePhoto" type="button">
                            <i class="trash icon"></i> Remove
                        </button>
                    </div>
                </div>
            </form>
        </div>
        <div class="actions">
            <button class="ui approve positive right labeled icon button" 
                    :class="{'loading': this.loading, 'disabled': !this.isValidForm() || this.loading}"
                    @click.prevent="handleSubmit" type="button">
                Update Photo
                <i class="camera icon"></i>
            </button>
        </div>
    </div>
    `
} 